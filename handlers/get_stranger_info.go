package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/hoshinonyaruko/gensokyo-kook/callapi"
	"github.com/hoshinonyaruko/gensokyo-kook/mylog"
)

func init() {
	callapi.RegisterHandler("get_stranger_info", GetStrangerInfo)
}

type StrangerInfoResponse struct {
	Data    StrangerInfoData `json:"data"`
	Message string           `json:"message"`
	RetCode int              `json:"retcode"`
	Status  string           `json:"status"`
	Echo    interface{}      `json:"echo"`
}

type StrangerInfoData struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex"`
	Age      int32  `json:"age"`
}

// GetStrangerInfo 实现 OneBot v11 get_stranger_info：
// 返回结构遵循标准字段 data.user_id / nickname / sex / age。
func GetStrangerInfo(client callapi.Client, Token string, BaseUrl string, message callapi.ActionMessage) (string, error) {
	userID, err := parseOneBotID(message.Params.UserID)
	if err != nil {
		mylog.Printf("get_stranger_info: invalid user_id %v: %v", message.Params.UserID, err)
		return "", nil
	}

	response := StrangerInfoResponse{
		Data: StrangerInfoData{
			UserID:   userID,
			Nickname: fmt.Sprintf("user_%d", userID),
			Sex:      "unknown",
			Age:      0,
		},
		Message: "",
		RetCode: 0,
		Status:  "ok",
		Echo:    message.Echo,
	}

	outputMap := structToMap(response)
	if err := client.SendMessage(outputMap); err != nil {
		mylog.Printf("error sending stranger info via wsclient: %v", err)
	}

	result, err := json.Marshal(response)
	if err != nil {
		mylog.Printf("Error marshaling data: %v", err)
		return "", nil
	}

	mylog.Printf("get_stranger_info: %s", result)
	return string(result), nil
}
