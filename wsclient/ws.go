package wsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hoshinonyaruko/gensokyo-mcp/botstats"
	"github.com/hoshinonyaruko/gensokyo-mcp/callapi"
	"github.com/hoshinonyaruko/gensokyo-mcp/config"
	"github.com/hoshinonyaruko/gensokyo-mcp/multid"
	"github.com/hoshinonyaruko/gensokyo-mcp/mylog"
	"github.com/hoshinonyaruko/gensokyo-mcp/praser"
)

var (
	// echoToChanMap 用于存储期待特定 echo 值的消息的通道
	echoToChanMap = make(map[string]chan callapi.ActionMessage)
	// generalChan 用于处理那些echo值不是字符串的消息
	generalChan = make(chan callapi.ActionMessage, 10)
	mapMutex    sync.Mutex
)

// pendingMessages：新增，用于存储超时后/重复的消息
var (
	pendingMutex    sync.Mutex
	pendingMessages = make(map[string][]callapi.ActionMessage)
)

type WebSocketClient struct {
	conn           *websocket.Conn
	botID          uint64
	urlStr         string
	cancel         context.CancelFunc
	isReconnecting bool
	sendFailures   []map[string]interface{} // 存储失败的消息
	writeCh        chan writeRequest        // 写请求通道
	closeCh        chan struct{}            // 用于关闭的通道
}

type writeRequest struct {
	messageType int
	data        []byte
}

// SendMessage 发送消息，将写请求发送到写 Goroutine
func (client *WebSocketClient) SendMessage(message map[string]interface{}) error {
	// 序列化消息
	msgBytes, err := json.Marshal(message)
	if err != nil {
		log.Println("Error marshalling message:", err)
		return err
	}

	// 创建专用通道，用于接收写操作的结果
	client.writeCh <- writeRequest{
		messageType: websocket.TextMessage,
		data:        msgBytes,
	}

	// 等待写操作完成，并返回结果
	return nil
}

// Close 关闭 WebSocketClient，停止写 Goroutine
func (client *WebSocketClient) Close() error {
	close(client.closeCh)
	close(client.writeCh)
	client.conn.Close()
	return nil
}

// startWriter 专用的写 Goroutine
func (client *WebSocketClient) startWriter() {
	for {
		select {
		case req := <-client.writeCh:
			// 执行写操作
			err := client.conn.WriteMessage(req.messageType, req.data)
			if err != nil {
				log.Println("Error sending message:", err)
				if !config.GetDisableErrorChan() {
					client.sendFailures = append(client.sendFailures, map[string]interface{}{"message": req.data}) // 记录失败的消息
				}
			}
		case <-client.closeCh:
			return
		}
	}
}

// 处理onebotv11应用端发来的信息
func (client *WebSocketClient) handleIncomingMessages(cancel context.CancelFunc) {
	for {
		_, msg, err := client.conn.ReadMessage()
		if err != nil {
			mylog.Println("WebSocket connection closed:", err)
			cancel() // 取消心跳 goroutine
			if !client.isReconnecting {
				go client.Reconnect()
			}
			return // 退出循环，不再尝试读取消息
		}

		go client.recvMessage(msg)
	}
}

// 断线重连
func (client *WebSocketClient) Reconnect() {
	client.isReconnecting = true

	addresses := config.GetWsAddress()
	tokens := config.GetWsToken()

	var token string
	for index, address := range addresses {
		if address == client.urlStr && index < len(tokens) {
			token = tokens[index]
			break
		}
	}

	// 检查URL中是否有access_token参数
	mp := getParamsFromURI(client.urlStr)
	if val, ok := mp["access_token"]; ok {
		token = val
	}

	headers := http.Header{
		"User-Agent":    []string{"CQHttp/4.15.0"},
		"X-Client-Role": []string{"Universal"},
		"X-Self-ID":     []string{fmt.Sprintf("%d", client.botID)},
	}

	if token != "" {
		headers["Authorization"] = []string{"Token " + token}
	}
	mylog.Printf("准备使用token[%s]重新连接到[%s]\n", token, client.urlStr)
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	var conn *websocket.Conn
	var err error

	maxRetryAttempts := config.GetReconnecTimes()
	retryCount := 0
	for {
		mylog.Println("Dialing URL:", client.urlStr)
		conn, _, err = dialer.Dial(client.urlStr, headers)
		if err != nil {
			retryCount++
			if retryCount > maxRetryAttempts {
				mylog.Printf("Exceeded maximum retry attempts for WebSocket[%v]: %v\n", client.urlStr, err)
				return
			}
			mylog.Printf("Failed to connect to WebSocket[%v]: %v, retrying in 5 seconds...\n", client.urlStr, err)
			time.Sleep(5 * time.Second) // sleep for 5 seconds before retrying
		} else {
			mylog.Printf("Successfully connected to %s.\n", client.urlStr) // 输出连接成功提示
			break                                                          // successfully connected, break the loop
		}
	}
	// 复用现有的client完成重连
	client.conn = conn

	// 再次发送元事件
	message := map[string]interface{}{
		"meta_event_type": "lifecycle",
		"post_type":       "meta_event",
		"self_id":         client.botID,
		"sub_type":        "connect",
		"time":            int(time.Now().Unix()),
	}

	mylog.Printf("Message: %+v\n", message)

	err = client.SendMessage(message)
	if err != nil {
		// handle error
		mylog.Printf("Error sending message: %v\n", err)
	}

	//退出老的sendHeartbeat和handleIncomingMessages
	client.cancel()

	// Starting goroutine for heartbeats and another for listening to messages
	ctx, cancel := context.WithCancel(context.Background())

	client.cancel = cancel
	heartbeatinterval := config.GetHeartBeatInterval()
	go client.sendHeartbeat(ctx, client.botID, heartbeatinterval)
	go client.handleIncomingMessages(cancel)

	defer func() {
		client.isReconnecting = false
	}()

	mylog.Printf("Successfully reconnected to WebSocket.")

}

// 处理发送失败的消息
func (client *WebSocketClient) processFailedMessages() {
	for _, failedMessage := range client.sendFailures {
		// 尝试重新发送消息
		err := client.SendMessage(failedMessage)
		if err != nil {
			mylog.Printf("Error resending message: %v\n", err)
		}
	}
	// 清空失败消息列表
	client.sendFailures = []map[string]interface{}{}
}

// 处理信息,调用腾讯api
func (client *WebSocketClient) recvMessage(msg []byte) {
	var message callapi.ActionMessage
	//mylog.Println("Received from onebotv11 server raw:", string(msg))
	err := json.Unmarshal(msg, &message)
	if err != nil {
		mylog.Printf("Error unmarshalling message: %v, Original message: %s", err, string(msg))
		return
	}
	mylog.Println("Received from onebotv11 server:", TruncateMessage(message, 800))

	// 判断Action是否以"send"开头
	if !strings.HasPrefix(message.Action, "send") {
		// 如果不是以"send"开头，记录日志并返回
		mylog.Printf("Action '%s' is not supported, ignored.", message.Action)
		client.respondToAction(message.Action, message.Echo)
		return
	}

	// string模式支持bind
	if config.GetStringOb11() {
		message.Params.UserID = multid.GetOriginIDFromActiveID(message.Params.UserID.(string))
	}

	mapMutex.Lock()
	defer mapMutex.Unlock()

	var echoKey string
	if !config.GetStringOb11() {
		echoKey = message.Params.UserID.(string)
	} else {
		// 如果双向Echo启用，根据echo的值处理消息
		echoKey = message.Params.UserID.(string)
	}

	if ch, ok := echoToChanMap[echoKey]; ok {
		// 如果找到匹配的信道，则发送消息
		ch <- message
		// 从映射中移除已处理的echo
		delete(echoToChanMap, echoKey)
	} else {
		pendingMutex.Lock()
		pendingMessages[echoKey] = append(pendingMessages[echoKey], message)
		pendingMutex.Unlock()
	}
}

// WaitForActionMessage 等待特定echo值的消息或超时
func WaitForActionMessage(userid string, timeout time.Duration) (*callapi.ActionMessage, error) {
	ch := make(chan callapi.ActionMessage, 1)

	mapMutex.Lock()
	echoToChanMap[userid] = ch
	mapMutex.Unlock()

	select {
	case msg := <-ch:
		return &msg, nil
	case <-time.After(timeout):
		mapMutex.Lock()
		delete(echoToChanMap, userid)
		mapMutex.Unlock()
		return nil, fmt.Errorf("timeout waiting for message with echo %s", userid)
	}
}

// 截断信息
func TruncateMessage(message callapi.ActionMessage, maxLength int) string {
	paramsStr, err := json.Marshal(message.Params)
	if err != nil {
		return "Error marshalling Params for truncation."
	}

	// Truncate Params if its length exceeds maxLength
	truncatedParams := string(paramsStr)
	if len(truncatedParams) > maxLength {
		truncatedParams = truncatedParams[:maxLength] + "..."
	}

	return fmt.Sprintf("Action: %s, Params: %s, Echo: %v", message.Action, truncatedParams, message.Echo)
}

// 发送心跳包
func (client *WebSocketClient) sendHeartbeat(ctx context.Context, botID uint64, heartbeatinterval int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(heartbeatinterval) * time.Second):
			messageReceived, messageSent, lastMessageTime, err := botstats.GetStats()
			if err != nil {
				mylog.Printf("心跳错误,获取机器人发信状态错误:%v", err)
			}
			message := map[string]interface{}{
				"post_type":       "meta_event",
				"meta_event_type": "heartbeat",
				"time":            int(time.Now().Unix()),
				"self_id":         botID,
				"status": map[string]interface{}{
					"app_enabled":     true,
					"app_good":        true,
					"app_initialized": true,
					"good":            true,
					"online":          true,
					"plugins_good":    nil,
					"stat": map[string]int{
						"packet_received":   34933,
						"packet_sent":       8513,
						"packet_lost":       0,
						"message_received":  messageReceived,
						"message_sent":      messageSent,
						"disconnect_times":  0,
						"lost_times":        0,
						"last_message_time": int(lastMessageTime),
					},
				},
				"interval": 5000, // 以毫秒为单位
			}
			client.SendMessage(message)
			// 重发失败的消息
			client.processFailedMessages()
		}
	}
}

// NewWebSocketClient 创建 WebSocketClient 实例，接受 WebSocket URL、botID
func NewWebSocketClient(urlStr string, botID uint64, maxRetryAttempts int) (*WebSocketClient, error) {
	addresses := config.GetWsAddress()
	tokens := config.GetWsToken()

	var token string
	for index, address := range addresses {
		if address == urlStr && index < len(tokens) {
			token = tokens[index]
			break
		}
	}

	// 检查URL中是否有access_token参数
	mp := getParamsFromURI(urlStr)
	if val, ok := mp["access_token"]; ok {
		token = val
	}

	headers := http.Header{
		"User-Agent":    []string{"CQHttp/4.15.0"},
		"X-Client-Role": []string{"Universal"},
		"X-Self-ID":     []string{fmt.Sprintf("%d", botID)},
	}

	if token != "" {
		headers["Authorization"] = []string{"Token " + token}
	}
	mylog.Printf("准备使用token[%s]连接到[%s]\n", token, urlStr)
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	var conn *websocket.Conn
	var err error

	retryCount := 0
	for {
		mylog.Println("Dialing URL:", urlStr)
		conn, _, err = dialer.Dial(urlStr, headers)
		if err != nil {
			retryCount++
			if retryCount > maxRetryAttempts {
				mylog.Printf("Exceeded maximum retry attempts for WebSocket[%v]: %v\n", urlStr, err)
				return nil, err
			}
			mylog.Printf("Failed to connect to WebSocket[%v]: %v, retrying in 5 seconds...\n", urlStr, err)
			time.Sleep(5 * time.Second) // sleep for 5 seconds before retrying
		} else {
			mylog.Printf("Successfully connected to %s.\n", urlStr) // 输出连接成功提示
			break                                                   // successfully connected, break the loop
		}
	}
	client := &WebSocketClient{
		conn:         conn,
		botID:        botID,
		urlStr:       urlStr,
		sendFailures: []map[string]interface{}{},
		writeCh:      make(chan writeRequest, 5000), // 缓冲区大小可以根据需求调整
		closeCh:      make(chan struct{}),
	}
	go client.startWriter() // 启动写 Goroutine

	// Sending initial message similar to your setupB function
	message := map[string]interface{}{
		"meta_event_type": "lifecycle",
		"post_type":       "meta_event",
		"self_id":         botID,
		"sub_type":        "connect",
		"time":            int(time.Now().Unix()),
	}

	mylog.Printf("Message: %+v\n", message)

	err = client.SendMessage(message)
	if err != nil {
		// handle error
		mylog.Printf("Error sending message: %v\n", err)
	}

	// Starting goroutine for heartbeats and another for listening to messages
	ctx, cancel := context.WithCancel(context.Background())

	client.cancel = cancel
	heartbeatinterval := config.GetHeartBeatInterval()
	go client.sendHeartbeat(ctx, botID, heartbeatinterval)
	go client.handleIncomingMessages(cancel)

	return client, nil
}

// getParamsFromURI 解析给定URI中的查询参数，并返回一个映射（map）
func getParamsFromURI(uriStr string) map[string]string {
	params := make(map[string]string)

	u, err := url.Parse(uriStr)
	if err != nil {
		mylog.Printf("Error parsing the URL: %v\n", err)
		return params
	}

	// 遍历查询参数并将其添加到返回的映射中
	for key, values := range u.Query() {
		if len(values) > 0 {
			params[key] = values[0] // 如果一个参数有多个值，这里只选择第一个。可以根据需求进行调整。
		}
	}

	return params
}

// respondToAction 根据action类型构造并发送响应消息
func (client *WebSocketClient) respondToAction(action string, echo interface{}) {
	var response map[string]interface{}

	switch action {
	case "get_group_list":
		response = make(map[string]interface{})
		data := make([]map[string]interface{}, 1) // 示例仅创建一个元素的数组
		for i := range data {
			data[i] = map[string]interface{}{
				"group_create_time": "0",
				"group_id":          "868858989",
				"group_level":       "0",
				"group_memo":        "",
				"group_name":        "可爱red",
				"max_member_count":  "3000",
				"member_count":      "1800",
			}
		}
		response["data"] = data
		response["message"] = ""
		response["retcode"] = 0
		response["status"] = "ok"
		response["echo"] = echo

	case "get_login_info":
		wxappidint := config.GetUinint64()
		response = map[string]interface{}{
			"data": map[string]interface{}{
				"nickname": "早苗",
				"user_id":  wxappidint,
			},
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	case "get_guild_service_profile":
		response = map[string]interface{}{
			"data": map[string]interface{}{
				"nickname": "",
				"tiny_id":  0,
			},
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	case "get_online_clients":
		response = map[string]interface{}{
			"data": map[string]interface{}{
				"clients": []interface{}{}, // 创建一个空的clients数组
				"tiny_id": 0,
			},
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
			"clients": []interface{}{}, // 根据描述，这可能是多余的，除非您有特定需求
		}

	case "get_version_info":
		response = map[string]interface{}{
			"data": map[string]interface{}{
				"app_full_name":              "go-cqhttp-v1.0.0_windows_amd64-go1.20.2",
				"app_name":                   "go-cqhttp",
				"app_version":                "v1.0.0",
				"coolq_directory":            "",
				"coolq_edition":              "pro",
				"go-cqhttp":                  true,
				"plugin_build_configuration": "release",
				"plugin_build_number":        99,
				"plugin_version":             "4.15.0",
				"protocol_name":              4,
				"protocol_version":           "v11",
				"runtime_os":                 "windows",
				"runtime_version":            "go1.20.2",
				"version":                    "v1.0.0",
			},
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	case "get_friend_list":
		friends := []map[string]interface{}{
			{"nickname": "小狐狸", "remark": "", "user_id": "2022717137"},
			// 添加更多好友信息...
		}
		response = map[string]interface{}{
			"data":    friends,
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	case "get_guild_list":
		data := []map[string]interface{}{}
		// 假设我们要添加一个示例公会信息，实际应用中这部分可能需要从数据库或其他数据源动态获取
		for i := 0; i < 1; i++ { // 示例仅添加一个公会
			data = append(data, map[string]interface{}{
				"guild_id":         "0",         // 公会ID示例值
				"guild_name":       "868858989", // 公会名称示例值
				"guild_display_id": "868858989", // 公会显示ID示例值
			})
		}
		response = map[string]interface{}{
			"data":    data,
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	case "get_guild_channel_list":
		// 这里示例不具体填充data数组中的频道信息，假设响应需要的是一个空的频道列表
		response = map[string]interface{}{
			"data":    []interface{}{}, // 创建一个空的频道列表
			"message": "",
			"retcode": 0,
			"status":  "ok",
			"echo":    echo,
		}

	default:
		mylog.Printf("Action '%s' is not supported, ignored.", action)
		return
	}

	err := client.SendMessage(response)
	if err != nil {
		mylog.Println("Error sending message:", err)
		return
	}

	mylog.Printf("Responded to action '%s' with: %v", action, response)
}

// GetPendingMessages：获取并删除最近的溢出消息，并检查字数是否超过2047
func GetPendingMessages(userid string, clear bool, currentLength int) ([]callapi.ActionMessage, int, error) {
	// 锁定 pendingMessages 保证并发安全
	pendingMutex.Lock()
	defer pendingMutex.Unlock()

	// 获取当前用户的所有溢出消息
	msgs := pendingMessages[userid]
	if len(msgs) == 0 {
		// 没有待处理的消息，直接返回
		return nil, currentLength, nil
	}

	// 结果数组，用于存储叠加的历史消息
	var pendingMsgsToReturn []callapi.ActionMessage
	totalLength := currentLength

	var ii int
	// 遍历所有历史消息
	for i := 0; i < len(msgs); i++ {
		// 获取当前消息（逐条处理）
		msg := msgs[i]

		// 将消息的内容提取出来
		var messageContent string
		if msgStr, ok := msg.Params.Message.(string); ok {
			messageContent = msgStr
		} else {
			// 如果不是 string 类型，调用解析函数处理
			messageContent = praser.ParseMessageContent(msg.Params.Message, true)
		}

		// 检查当前字数是否超过2047
		if totalLength+len(messageContent)+len("-----历史信息----") > 2047 {
			// 如果叠加后超出字数限制，则停止叠加
			// 删除该条消息
			pendingMessages[userid] = append(msgs[:i], msgs[i+1:]...)
			break
		}

		// 累加历史信息
		pendingMsgsToReturn = append(pendingMsgsToReturn, msg)
		totalLength += len(messageContent) + len("-----历史信息----")
		ii++
	}

	// 如果需要清空历史消息，将其清除
	if clear {
		// 使用切片删除已处理的消息，确保更新 pendingMessages
		pendingMessages[userid] = msgs[ii:]
	}

	// 返回叠加的历史消息和当前总字数
	return pendingMsgsToReturn, totalLength, nil
}

// AddMessageToPending 向指定 echoKey 对应的 pendingMessages 中添加一条消息
func AddMessageToPending(echoKey string, message *callapi.ActionMessage) {
	// 锁定 pendingMessages 保证并发安全
	pendingMutex.Lock()
	defer pendingMutex.Unlock()

	// 将消息添加到对应的 echoKey 的 pendingMessages 中
	pendingMessages[echoKey] = append(pendingMessages[echoKey], *message)

	// 打印日志，确保信息已被添加
	mylog.Printf("Added message to pendingMessages for echoKey '%s'. Current queue length: %d", echoKey, len(pendingMessages[echoKey]))
}
