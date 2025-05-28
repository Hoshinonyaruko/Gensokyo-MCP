// 处理收到的信息事件
package Processor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/hoshinonyaruko/gensokyo-mcp/callapi"
	"github.com/hoshinonyaruko/gensokyo-mcp/mylog"
	"github.com/hoshinonyaruko/gensokyo-mcp/structs"
	"github.com/hoshinonyaruko/gensokyo-mcp/wsclient"
)

// Processor 结构体用于处理消息
type Processors struct {
	Settings        *structs.Settings                 // 使用指针
	Wsclient        []*wsclient.WebSocketClient       // 指针的切片
	WsServerClients []callapi.WebSocketServerClienter //ws server被连接的客户端
}

type Sender struct {
	Nickname string `json:"nickname"`
	TinyID   string `json:"tiny_id"`
	UserID   int64  `json:"user_id"`
	Role     string `json:"role,omitempty"`
	Card     string `json:"card,omitempty"`
	Sex      string `json:"sex,omitempty"`
	Age      int32  `json:"age,omitempty"`
	Area     string `json:"area,omitempty"`
	Level    string `json:"level,omitempty"`
	Title    string `json:"title,omitempty"`
}

// 频道信息事件
type OnebotChannelMessage struct {
	ChannelID       string      `json:"channel_id"`
	GuildID         string      `json:"guild_id"`
	Message         interface{} `json:"message"`
	MessageID       string      `json:"message_id"`
	MessageType     string      `json:"message_type"`
	PostType        string      `json:"post_type"`
	SelfID          int64       `json:"self_id"`
	SelfTinyID      string      `json:"self_tiny_id"`
	Sender          Sender      `json:"sender"`
	SubType         string      `json:"sub_type"`
	Time            int64       `json:"time"`
	Avatar          string      `json:"avatar,omitempty"`
	UserID          int64       `json:"user_id"`
	RawMessage      string      `json:"raw_message"`
	Echo            string      `json:"echo,omitempty"`
	RealMessageType string      `json:"real_message_type,omitempty"` //当前信息的真实类型 表情表态
}

// 群信息事件
type OnebotGroupMessage struct {
	RawMessage      string      `json:"raw_message"`
	MessageID       int         `json:"message_id"`
	GroupID         int64       `json:"group_id"` // Can be either string or int depending on p.Settings.CompleteFields
	MessageType     string      `json:"message_type"`
	PostType        string      `json:"post_type"`
	SelfID          int64       `json:"self_id"` // Can be either string or int
	Sender          Sender      `json:"sender"`
	SubType         string      `json:"sub_type"`
	Time            int64       `json:"time"`
	Avatar          string      `json:"avatar,omitempty"`
	Echo            string      `json:"echo,omitempty"`
	Message         interface{} `json:"message"` // For array format
	MessageSeq      int         `json:"message_seq"`
	Font            int         `json:"font"`
	UserID          int64       `json:"user_id"`
	RealMessageType string      `json:"real_message_type,omitempty"`  //当前信息的真实类型 group group_private guild guild_private
	RealUserID      string      `json:"real_user_id,omitempty"`       //当前真实uid
	RealGroupID     string      `json:"real_group_id,omitempty"`      //当前真实gid
	IsBindedGroupId bool        `json:"is_binded_group_id,omitempty"` //当前群号是否是binded后的
	IsBindedUserId  bool        `json:"is_binded_user_id,omitempty"`  //当前用户号号是否是binded后的
}

type OnebotGroupMessageS struct {
	RawMessage      string      `json:"raw_message"`
	MessageID       string      `json:"message_id"`
	GroupID         string      `json:"group_id"` // Can be either string or int depending on p.Settings.CompleteFields
	MessageType     string      `json:"message_type"`
	PostType        string      `json:"post_type"`
	SelfID          int64       `json:"self_id"` // Can be either string or int
	Sender          Sender      `json:"sender"`
	SubType         string      `json:"sub_type"`
	Time            int64       `json:"time"`
	Avatar          string      `json:"avatar,omitempty"`
	Echo            string      `json:"echo,omitempty"`
	Message         interface{} `json:"message"` // For array format
	MessageSeq      int         `json:"message_seq"`
	Font            int         `json:"font"`
	UserID          string      `json:"user_id"`
	RealMessageType string      `json:"real_message_type,omitempty"`  //当前信息的真实类型 group group_private guild guild_private
	RealUserID      string      `json:"real_user_id,omitempty"`       //当前真实uid
	RealGroupID     string      `json:"real_group_id,omitempty"`      //当前真实gid
	IsBindedGroupId bool        `json:"is_binded_group_id,omitempty"` //当前群号是否是binded后的
	IsBindedUserId  bool        `json:"is_binded_user_id,omitempty"`  //当前用户号号是否是binded后的
}

// 私聊信息事件
type OnebotPrivateMessage struct {
	RawMessage      string        `json:"raw_message"`
	MessageID       int           `json:"message_id"` // Can be either string or int depending on logic
	MessageType     string        `json:"message_type"`
	PostType        string        `json:"post_type"`
	SelfID          int64         `json:"self_id"` // Can be either string or int depending on logic
	Sender          PrivateSender `json:"sender"`
	SubType         string        `json:"sub_type"`
	Time            int64         `json:"time"`
	Avatar          string        `json:"avatar,omitempty"`
	Echo            string        `json:"echo,omitempty"`
	Message         interface{}   `json:"message"`                     // For array format
	MessageSeq      int           `json:"message_seq"`                 // Optional field
	Font            int           `json:"font"`                        // Optional field
	UserID          int64         `json:"user_id"`                     // Can be either string or int depending on logic
	RealMessageType string        `json:"real_message_type,omitempty"` //当前信息的真实类型 group group_private guild guild_private
	RealUserID      string        `json:"real_user_id,omitempty"`      //当前真实uid
	IsBindedUserId  bool          `json:"is_binded_user_id,omitempty"` //当前用户号号是否是binded后的
}

type PrivateSender struct {
	Nickname string `json:"nickname"`
	UserID   int64  `json:"user_id"` // Can be either string or int depending on logic
}

// 打印结构体的函数
func PrintStructWithFieldNames(v interface{}) {
	val := reflect.ValueOf(v)

	// 如果是指针，获取其指向的元素
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	typ := val.Type()

	// 确保我们传入的是一个结构体
	if typ.Kind() != reflect.Struct {
		mylog.Println("Input is not a struct")
		return
	}

	// 迭代所有的字段并打印字段名和值
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		value := val.Field(i)
		mylog.Printf("%s: %v\n", field.Name, value.Interface())
	}
}

// 将结构体转换为 map[string]interface{}
func structToMap(obj interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	j, _ := json.Marshal(obj)
	json.Unmarshal(j, &out)
	return out
}

// 修改函数的返回类型为 *Processor
func NewProcessor(settings *structs.Settings, wsclient []*wsclient.WebSocketClient) *Processors {
	return &Processors{
		Settings: settings,
		Wsclient: wsclient,
	}
}

// 修改函数的返回类型为 *Processor
func NewProcessorV2(settings *structs.Settings) *Processors {
	return &Processors{
		Settings: settings,
	}
}

// 发信息给所有连接正向ws的客户端
func (p *Processors) SendMessageToAllClients(message map[string]interface{}) error {
	var result *multierror.Error

	for _, client := range p.WsServerClients {
		// 使用接口的方法
		err := client.SendMessage(message)
		if err != nil {
			// Append the error to our result
			result = multierror.Append(result, fmt.Errorf("failed to send to client: %w", err))
		}
	}

	// This will return nil if no errors were added
	return result.ErrorOrNil()
}

// 方便快捷的发信息函数
func (p *Processors) BroadcastMessageToAllFAF(message map[string]interface{}, data interface{}) error {
	// 并发发送到我们作为客户端的Wsclient
	for _, client := range p.Wsclient {
		go func(c callapi.WebSocketServerClienter) {
			_ = c.SendMessage(message) // 忽略错误
		}(client)
	}

	// 并发发送到我们作为服务器连接到我们的WsServerClients
	for _, serverClient := range p.WsServerClients {
		go func(sc callapi.WebSocketServerClienter) {
			_ = sc.SendMessage(message) // 忽略错误
		}(serverClient)
	}

	// 不再等待所有 goroutine 完成，直接返回
	return nil
}

// 方便快捷的发信息函数
func (p *Processors) BroadcastMessageToAll(message map[string]interface{}, data interface{}) error {
	var wg sync.WaitGroup
	errorCh := make(chan string, len(p.Wsclient)+len(p.WsServerClients))
	defer close(errorCh)

	// 并发发送到我们作为客户端的Wsclient
	for _, client := range p.Wsclient {
		wg.Add(1)
		go func(c callapi.WebSocketServerClienter) {
			defer wg.Done()
			if err := c.SendMessage(message); err != nil {
				errorCh <- fmt.Sprintf("error sending message via wsclient: %v", err)
			}
		}(client)
	}

	// 并发发送到我们作为服务器连接到我们的WsServerClients
	for _, serverClient := range p.WsServerClients {
		wg.Add(1)
		go func(sc callapi.WebSocketServerClienter) {
			defer wg.Done()
			if err := sc.SendMessage(message); err != nil {
				errorCh <- fmt.Sprintf("error sending message via WsServerClient: %v", err)
			}
		}(serverClient)
	}

	wg.Wait() // 等待所有goroutine完成

	var errors []string
	failed := 0
	for len(errorCh) > 0 {
		err := <-errorCh
		errors = append(errors, err)
		failed++
	}

	if len(errors) > 0 {
		return fmt.Errorf(strings.Join(errors, "; "))
	}

	return nil
}

// 方便快捷的发信息函数
func BroadcastMessageToAll(message map[string]interface{}, Wsclient []*wsclient.WebSocketClient) error {
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
