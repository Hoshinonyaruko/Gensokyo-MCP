// 处理收到的信息事件
package Processor

import (
	"strconv"
	"time"

	"github.com/hoshinonyaruko/gensokyo-mcp/config"
	"github.com/hoshinonyaruko/gensokyo-mcp/handlers"
	"github.com/hoshinonyaruko/gensokyo-mcp/wsclient"
	"github.com/mark3labs/mcp-go/mcp"
)

// ProcessGroupMessage 处理群组消息
func ProcessGroupMessage(data mcp.CallToolRequest, Wsclient []*wsclient.WebSocketClient) (err error) {

	selfid := config.GetUinint64()

	var args struct {
		Payload string `json:"payload"`
		UserID  string `json:"user_id"`
		GroupID string `json:"group_id"`
		Timeout int    `json:"timeout"`
		Bearer  int64  `json:"bearer,omitempty"`
	}
	if err := data.BindArguments(&args); err != nil {
		return err
	}
	if args.Payload == "" {
		args.Payload = "帮助"
	}

	//p.HandleFrameworkCommand(messageText, data, "group")
	messageText := args.Payload

	// 如果在Array模式下, 则处理Message为Segment格式
	var segmentedMessages interface{} = messageText
	if config.GetArrayValue() {
		segmentedMessages = handlers.ConvertToSegmentedMessage(messageText)
	}
	var IsBindedUserId, IsBindedGroupId bool

	// 是否使用string形式上报
	if !config.GetStringOb11() {

		intGroup, _ := strconv.Atoi(args.GroupID)
		intUser, _ := strconv.Atoi(args.UserID)

		groupMsg := OnebotGroupMessage{
			RawMessage:  messageText,
			Message:     segmentedMessages,
			MessageID:   123,
			GroupID:     int64(intGroup),
			MessageType: "group",
			PostType:    "message",
			SelfID:      selfid,
			UserID:      int64(intUser),
			Sender: Sender{
				UserID: int64(intUser),
				Sex:    "0",
				Age:    0,
				Area:   "0",
				Level:  "0",
			},
			SubType: "normal",
			Time:    time.Now().Unix(),
		}
		//增强配置
		if !config.GetNativeOb11() {
			groupMsg.RealMessageType = "group"
			groupMsg.IsBindedUserId = IsBindedUserId
			groupMsg.IsBindedGroupId = IsBindedGroupId
			// if IsBindedUserId {
			// 	//groupMsg.Avatar, _ = GenerateAvatarURL(userid64)
			// }
		}

		// 调试
		PrintStructWithFieldNames(groupMsg)

		// Convert OnebotGroupMessage to map and send
		groupMsgMap := structToMap(groupMsg)
		//上报信息到onebotv11应用端(正反ws)
		BroadcastMessageToAll(groupMsgMap, Wsclient)
	} else {

		groupMsg := OnebotGroupMessageS{
			RawMessage:  messageText,
			Message:     segmentedMessages,
			MessageID:   "",
			GroupID:     args.GroupID,
			MessageType: "group",
			PostType:    "message",
			SelfID:      selfid,
			UserID:      args.UserID,
			Sender: Sender{
				UserID: 0,
				Sex:    "0",
				Age:    0,
				Area:   "0",
				Level:  "0",
			},
			SubType:     "normal",
			Time:        time.Now().Unix(),
			RealGroupID: args.GroupID,
			RealUserID:  args.UserID,
		}

		//增强配置
		if !config.GetNativeOb11() {
			groupMsg.RealMessageType = "group"
			groupMsg.IsBindedUserId = IsBindedUserId
			groupMsg.IsBindedGroupId = IsBindedGroupId
			// if IsBindedUserId {
			// 	//groupMsg.Avatar, _ = GenerateAvatarURL(userid64)
			// }
		}

		// 调试
		PrintStructWithFieldNames(groupMsg)

		// Convert OnebotGroupMessage to map and send
		groupMsgMap := structToMap(groupMsg)
		//上报信息到onebotv11应用端(正反ws)
		BroadcastMessageToAll(groupMsgMap, Wsclient)
	}

	return err
}
