package handlers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/hoshinonyaruko/gensokyo-kook/callapi"
	"github.com/hoshinonyaruko/gensokyo-kook/mylog"
)

func parseOneBotID(id interface{}) (int64, error) {
	switch v := id.(type) {
	case string:
		return strconv.ParseInt(v, 10, 64)
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("unsupported id type: %T", id)
	}
}

// 初始化handler，在程序启动时会被调用
func init() {
	callapi.RegisterHandler("get_group_member_info", GetGroupMemberInfo)
}

// 成员信息的结构定义
type MemberInfo struct {
	UserID          int64  `json:"user_id"`
	GroupID         int64  `json:"group_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int32  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int32  `json:"join_time"`
	LastSentTime    int32  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"`
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int64  `json:"title_expire_time"`
	CardChangeable  bool   `json:"card_changeable"`
	ShutUpTimestamp int64  `json:"shut_up_timestamp"`
}

// 构建单个成员的响应数据
func buildResponseForSingleMember(memberInfo *MemberInfo, echoValue interface{}) map[string]interface{} {
	// 构建成员数据的映射
	memberMap := map[string]interface{}{
		"group_id":          memberInfo.GroupID,
		"user_id":           memberInfo.UserID,
		"nickname":          memberInfo.Nickname,
		"card":              memberInfo.Card,
		"sex":               memberInfo.Sex,
		"age":               memberInfo.Age,
		"area":              memberInfo.Area,
		"join_time":         memberInfo.JoinTime,
		"last_sent_time":    memberInfo.LastSentTime,
		"level":             memberInfo.Level,
		"role":              memberInfo.Role,
		"unfriendly":        memberInfo.Unfriendly,
		"title":             memberInfo.Title,
		"title_expire_time": memberInfo.TitleExpireTime,
		"card_changeable":   memberInfo.CardChangeable,
		"shut_up_timestamp": memberInfo.ShutUpTimestamp,
	}

	// 构建完整的响应映射
	response := map[string]interface{}{
		"retcode": 0,
		"status":  "ok",
		"data":    memberMap,
		"echo":    echoValue,
	}

	return response
}

// getGroupMemberInfo是处理获取群成员信息的函数
func GetGroupMemberInfo(client callapi.Client, Token string, BaseUrl string, message callapi.ActionMessage) (string, error) {
	userID, err := parseOneBotID(message.Params.UserID)
	if err != nil {
		mylog.Printf("get_group_member_info: invalid user_id %v: %v", message.Params.UserID, err)
		return "", nil
	}

	groupID, err := parseOneBotID(message.Params.GroupID)
	if err != nil {
		mylog.Printf("get_group_member_info: invalid group_id %v: %v", message.Params.GroupID, err)
		return "", nil
	}

	// 使用请求参数里的真实 user_id/group_id 构造响应，避免返回固定占位符 ID。
	now := int32(time.Now().Unix())
	memberInfo := &MemberInfo{
		UserID:          userID,
		GroupID:         groupID,
		Nickname:        "主人", // 虚拟昵称
		Card:            "主人",
		Sex:             "unknown", // 性别未知
		Age:             20,        // 虚拟年龄
		Area:            "虚拟地区",
		JoinTime:        now,
		LastSentTime:    now,
		Level:           "1",      // 虚拟成员等级
		Role:            "member", // 角色为普通成员
		Unfriendly:      false,    // 没有不良记录
		Title:           "虚拟头衔",
		TitleExpireTime: 0,
		CardChangeable:  true, // 允许修改群名片
		ShutUpTimestamp: 0,    // 不在禁言中
	}

	// 构建响应JSON
	responseJSON := buildResponseForSingleMember(memberInfo, message.Echo)
	mylog.Printf("get_group_member_info: %s\n", responseJSON)

	// 发送响应回去
	err = client.SendMessage(responseJSON)
	if err != nil {
		mylog.Printf("发送消息时出错: %v", err)
	}
	result, err := ConvertMapToJSONString(responseJSON)
	if err != nil {
		mylog.Printf("Error marshaling data: %v", err)
		//todo 符合onebotv11 ws返回的错误码
		return "", nil
	}
	return string(result), nil
}
