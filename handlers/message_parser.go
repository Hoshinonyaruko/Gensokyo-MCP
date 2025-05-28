package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hoshinonyaruko/gensokyo-mcp/callapi"
	"github.com/hoshinonyaruko/gensokyo-mcp/wsclient"
	//xurls是一个从文本提取url的库 适用于多种场景
)

var BotID string
var AppID string

// 定义响应结构体
type ServerResponse struct {
	Data struct {
		MessageID int `json:"message_id"`
	} `json:"data"`
	Message string      `json:"message"`
	RetCode int         `json:"retcode"`
	Status  string      `json:"status"`
	Echo    interface{} `json:"echo"`
}

// SendResponse 向所有连接的WebSocket客户端广播回执信息
func SendResponse(Wsclient []*wsclient.WebSocketClient, err error, message *callapi.ActionMessage) (string, error) {
	// 初始化响应结构体
	response := ServerResponse{}
	response.Data.MessageID = 123 // TODO: 实现messageID转换
	response.Echo = message.Echo
	if err != nil {
		response.Message = err.Error()
		response.RetCode = 0 // 示例中将错误情况也视为操作成功
		response.Status = "ok"
	} else {
		response.Message = "操作成功"
		response.RetCode = 0
		response.Status = "ok"
	}

	// 将响应结构体转换为JSON字符串
	jsonResponse, jsonErr := json.Marshal(response)
	if jsonErr != nil {
		log.Printf("Error marshaling response to JSON: %v", jsonErr)
		return "", jsonErr
	}

	// 准备发送的消息
	messageMap := make(map[string]interface{})
	if err := json.Unmarshal(jsonResponse, &messageMap); err != nil {
		log.Printf("Error unmarshaling JSON response: %v", err)
		return "", err
	}

	// 广播消息到所有Wsclient
	broadcastErr := broadcastMessageToAll(messageMap, Wsclient)
	if broadcastErr != nil {
		log.Printf("Error broadcasting message to all clients: %v", broadcastErr)
		return "", broadcastErr
	}

	log.Printf("发送成功回执: %+v", string(jsonResponse))
	return string(jsonResponse), nil
}

// 方便快捷的发信息函数
func broadcastMessageToAll(message map[string]interface{}, Wsclient []*wsclient.WebSocketClient) error {
	var errors []string

	// 发送到我们作为客户端的Wsclient
	for _, client := range Wsclient {
		//mylog.Printf("第%v个Wsclient", test)
		err := client.SendMessage(message)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error sending private message via wsclient: %v", err))
		}
	}

	// 在循环结束后处理记录的错误
	if len(errors) > 0 {
		return fmt.Errorf(strings.Join(errors, "; "))
	}
	return nil
}

// allEmpty checks if all the strings in the slice are empty.
func allEmpty(addresses []string) bool {
	for _, addr := range addresses {
		if addr != "" {
			return false
		}
	}
	return true
}

// 将map转化为json string
func ConvertMapToJSONString(m map[string]interface{}) (string, error) {
	// 使用 json.Marshal 将 map 转换为 JSON 字节切片
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		log.Printf("Error marshalling map to JSON: %v", err)
		return "", err
	}

	// 将字节切片转换为字符串
	jsonString := string(jsonBytes)
	return jsonString, nil
}

// 将收到的data.content转换为message segment todo,群场景不支持受图片,频道场景的图片可以拼一下
func ConvertToSegmentedMessage(data string) []map[string]interface{} {

	var messageSegments []map[string]interface{}

	// 内容被视为文本部分
	if data != "" {
		textSegment := map[string]interface{}{
			"type": "text",
			"data": map[string]interface{}{
				"text": data,
			},
		}
		messageSegments = append(messageSegments, textSegment)
	}
	//排列
	messageSegments = sortMessageSegments(messageSegments)
	return messageSegments
}

// ConvertToInt64 尝试将 interface{} 类型的值转换为 int64 类型
func ConvertToInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		// 当无法处理该类型时返回错误
		return 0, fmt.Errorf("无法将类型 %T 转换为 int64", value)
	}
}

// 排列MessageSegments
func sortMessageSegments(segments []map[string]interface{}) []map[string]interface{} {
	var atSegments, textSegments, imageSegments []map[string]interface{}

	for _, segment := range segments {
		switch segment["type"] {
		case "at":
			atSegments = append(atSegments, segment)
		case "text":
			textSegments = append(textSegments, segment)
		case "image":
			imageSegments = append(imageSegments, segment)
		}
	}

	// 按照指定的顺序合并这些切片
	return append(append(atSegments, textSegments...), imageSegments...)
}
