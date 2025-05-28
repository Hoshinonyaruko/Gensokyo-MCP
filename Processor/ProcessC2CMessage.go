// 处理收到的信息事件
package Processor

import (
	"time"

	"github.com/hoshinonyaruko/gensokyo-mcp/config"
	"github.com/hoshinonyaruko/gensokyo-mcp/handlers"
	"github.com/hoshinonyaruko/gensokyo-mcp/wsclient"
	"github.com/mark3labs/mcp-go/mcp"
)

// ProcessC2CMessage 处理C2C消息 群私聊
func ProcessC2CMessage(data mcp.CallToolRequest, bearer int64, Wsclient []*wsclient.WebSocketClient) (err error) {
	// 打印data结构体
	PrintStructWithFieldNames(data)

	// 从私信中提取必要的信息 这是测试回复需要用到
	//recipientID := data.Author.ID
	//ChannelID := data.ChannelID
	//sourece是源头频道
	//GuildID := data.GuildID

	// 直接转换成ob11私信

	selfid := config.GetUinint64()

	//收到私聊信息调用的具体还原步骤
	//1,idmap还原真实userid,
	//发信息使用的是userid

	//转换at
	// messageText := handlers.RevertTransformedText(data, "group_private", p.Api, p.Apiv2, userid64, userid64, config.GetWhiteEnable(5))
	// if messageText == "" {
	// 	mylog.Printf("信息被自定义黑白名单拦截")
	// 	return nil
	// }
	//框架内指令
	//p.HandleFrameworkCommand(messageText, data, "group_private")

	messageText := data.Params.Arguments.(string)
	//如果在Array模式下, 则处理Message为Segment格式
	var segmentedMessages interface{} = messageText
	if config.GetArrayValue() {
		segmentedMessages = handlers.ConvertToSegmentedMessage(messageText)
	}
	var IsBindedUserId bool
	// if config.GetHashIDValue() {
	// 	IsBindedUserId = idmap.CheckValue(data.Author.ID, userid64)
	// } else {
	// 	IsBindedUserId = idmap.CheckValuev2(userid64)
	// }

	privateMsg := OnebotPrivateMessage{
		RawMessage:  messageText,
		Message:     segmentedMessages,
		MessageID:   123,
		MessageType: "private",
		PostType:    "message",
		SelfID:      selfid,
		UserID:      bearer,
		Sender: PrivateSender{
			Nickname: "", //这个不支持,但加机器人好友,会收到一个事件,可以对应储存获取,用idmaps可以做到.
			UserID:   bearer,
		},
		SubType: "friend",
		Time:    time.Now().Unix(),
	}
	if !config.GetNativeOb11() {
		privateMsg.RealMessageType = "group_private"
		privateMsg.IsBindedUserId = IsBindedUserId
		// if IsBindedUserId {
		// 	//privateMsg.Avatar, _ = GenerateAvatarURL(userid64)
		// }
	}

	// 调试
	PrintStructWithFieldNames(privateMsg)

	// Convert OnebotGroupMessage to map and send
	privateMsgMap := structToMap(privateMsg)
	//上报信息到onebotv11应用端(正反ws)
	BroadcastMessageToAll(privateMsgMap, Wsclient)
	return err
}
