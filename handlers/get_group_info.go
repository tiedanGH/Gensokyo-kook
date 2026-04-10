package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/hoshinonyaruko/gensokyo-kook/callapi"
	"github.com/hoshinonyaruko/gensokyo-kook/idmap"
	"github.com/hoshinonyaruko/gensokyo-kook/mylog"
	"github.com/idodo/golang-bot/kaihela/api/helper"
)

func init() {
	callapi.RegisterHandler("get_group_info", HandleGetGroupInfo)
}

type OnebotGroupInfo struct {
	Data    GroupInfo    `json:"data"`
	Message string      `json:"message"`
	RetCode int         `json:"retcode"`
	Status  string      `json:"status"`
	Echo    interface{} `json:"echo"`
}

type GroupInfo struct {
	GroupID         int64  `json:"group_id"`
	GroupName       string `json:"group_name"`
	GroupMemo       string `json:"group_memo"`
	GroupCreateTime int32  `json:"group_create_time"`
	GroupLevel      int32  `json:"group_level"`
	MemberCount     int32  `json:"member_count"`
	MaxMemberCount  int32  `json:"max_member_count"`
}

func HandleGetGroupInfo(client callapi.Client, Token string, BaseUrl string, message callapi.ActionMessage) (string, error) {
	var groupIDStr string
	switch v := message.Params.GroupID.(type) {
	case string:
		groupIDStr = v
	case float64:
		groupIDStr = fmt.Sprintf("%.0f", v)
	default:
		mylog.Printf("get_group_info: invalid group_id type: %T", message.Params.GroupID)
		return "", nil
	}

	if groupIDStr == "" {
		mylog.Printf("get_group_info: group_id is empty")
		return "", nil
	}

	// Try to reverse-map the int64 group_id back to the original KOOK channel ID
	realID, err := idmap.RetrieveRowByIDv2(groupIDStr)
	if err != nil {
		mylog.Printf("get_group_info: error retrieving real ID for %s: %v", groupIDStr, err)
		// If reverse lookup fails, treat groupIDStr as-is (it may already be a raw KOOK guild ID)
		realID = groupIDStr
	}

	// Check if this ID has an associated guild_id (i.e., it's a channel mapped to a guild)
	guildID, err := idmap.ReadConfigv2(realID, "guild_id")
	isChannel := err == nil && guildID != ""

	if !isChannel {
		// No guild_id stored — this might be a guild ID itself
		guildID = realID
	}

	var groupName string
	var groupMemo string
	var groupLevel int32

	if isChannel {
		// realID is a KOOK channel ID — fetch channel info via /v3/channel/view
		channelAPI := helper.NewApiHelper("/v3/channel/view", Token, BaseUrl, "", "")
		channelAPI.SetQuery(map[string]string{
			"target_id": realID,
		})
		channelResp, err := channelAPI.Get()
		if err != nil {
			mylog.Printf("get_group_info: error fetching channel view for %s: %v", realID, err)
			return "", nil
		}

		var channelViewResp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Topic   string `json:"topic"`
				GuildID string `json:"guild_id"`
				Type    int    `json:"type"`
				Level   int    `json:"level"`
			} `json:"data"`
		}
		if err := json.Unmarshal(channelResp, &channelViewResp); err != nil {
			mylog.Printf("get_group_info: error unmarshaling channel view response: %v", err)
			return "", nil
		}
		if channelViewResp.Code != 0 {
			mylog.Printf("get_group_info: KOOK channel API error code %d: %s", channelViewResp.Code, channelViewResp.Message)
			return "", nil
		}

		groupName = channelViewResp.Data.Name
		groupMemo = channelViewResp.Data.Topic
		groupLevel = int32(channelViewResp.Data.Level)
	} else {
		// realID is a KOOK guild ID — fetch guild info via /v3/guild/view
		guildAPI := helper.NewApiHelper("/v3/guild/view", Token, BaseUrl, "", "")
		guildAPI.SetQuery(map[string]string{
			"guild_id": guildID,
		})
		guildResp, err := guildAPI.Get()
		if err != nil {
			mylog.Printf("get_group_info: error fetching guild view for %s: %v", guildID, err)
			return "", nil
		}

		var guildViewResp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Topic string `json:"topic"`
				Level int    `json:"level"`
			} `json:"data"`
		}
		if err := json.Unmarshal(guildResp, &guildViewResp); err != nil {
			mylog.Printf("get_group_info: error unmarshaling guild view response: %v", err)
			return "", nil
		}
		if guildViewResp.Code != 0 {
			mylog.Printf("get_group_info: KOOK guild API error code %d: %s", guildViewResp.Code, guildViewResp.Message)
			return "", nil
		}

		groupName = "*" + guildViewResp.Data.Name // 与 get_group_list 保持一致，服务器名称加 * 前缀
		groupMemo = guildViewResp.Data.Topic
		groupLevel = int32(guildViewResp.Data.Level)
	}

	// Build the group_id for the response: use the original requested ID
	groupID64, _ := strconv.ParseInt(groupIDStr, 10, 64)

	groupInfo := &OnebotGroupInfo{
		Data: GroupInfo{
			GroupID:         groupID64,
			GroupName:       groupName,
			GroupMemo:       groupMemo,
			GroupCreateTime: int32(time.Now().Unix()),
			GroupLevel:      groupLevel,
			MemberCount:     0,
			MaxMemberCount:  0,
		},
		Message: "success",
		RetCode: 0,
		Status:  "ok",
	}

	if message.Echo == "" {
		groupInfo.Echo = "0"
	} else {
		groupInfo.Echo = message.Echo
	}

	groupInfoMap := structToMap(groupInfo)

	mylog.Printf("get_group_info: %+v", groupInfoMap)

	err = client.SendMessage(groupInfoMap)
	if err != nil {
		mylog.Printf("get_group_info: error sending message via client: %v", err)
	}

	result, err := json.Marshal(groupInfo)
	if err != nil {
		mylog.Printf("get_group_info: error marshaling data: %v", err)
		return "", nil
	}

	return string(result), nil
}

// structToMap 将结构体转换为 map[string]interface{}
func structToMap(obj interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	j, _ := json.Marshal(obj)
	json.Unmarshal(j, &out)
	return out
}
