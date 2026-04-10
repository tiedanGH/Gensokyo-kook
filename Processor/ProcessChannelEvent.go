// 处理频道增删事件，转换为 OneBot v11 群增减通知
package Processor

import (
	"time"

	"github.com/hoshinonyaruko/gensokyo-kook/config"
	"github.com/hoshinonyaruko/gensokyo-kook/idmap"
	"github.com/hoshinonyaruko/gensokyo-kook/mylog"
)

// GroupNoticeEvent 表示 OneBot v11 群通知事件
type GroupNoticeEvent struct {
	GroupID    int64  `json:"group_id"`
	NoticeType string `json:"notice_type"`
	OperatorID int64  `json:"operator_id"`
	PostType   string `json:"post_type"`
	SelfID     int64  `json:"self_id"`
	SubType    string `json:"sub_type"`
	Time       int64  `json:"time"`
	UserID     int64  `json:"user_id"`
}

// ChannelAddedBody 表示 KOOK added_channel 事件的 extra.body
type ChannelAddedBody struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UserID     string `json:"user_id"`
	GuildID    string `json:"guild_id"`
	Type       int    `json:"type"`
	ParentID   string `json:"parent_id"`
	IsCategory int    `json:"is_category"`
}

// ChannelDeletedBody 表示 KOOK deleted_channel 事件的 extra.body
type ChannelDeletedBody struct {
	ID        string `json:"id"`
	DeletedAt int64  `json:"deleted_at"`
	Type      int    `json:"type"`
}

// ProcessChannelAdded 处理 KOOK added_channel 事件，上报 OneBot group_increase 通知
func (p *Processors) ProcessChannelAdded(body *ChannelAddedBody) error {
	// 跳过分类（is_category=1）和非文字频道（type!=1）
	if body.IsCategory == 1 {
		mylog.Printf("ProcessChannelAdded: 跳过分类频道 %s", body.ID)
		return nil
	}
	if body.Type != 1 {
		mylog.Printf("ProcessChannelAdded: 跳过非文字频道 %s (type=%d)", body.ID, body.Type)
		return nil
	}

	// 映射 KOOK channel ID 到 int64 group_id
	groupID64, err := idmap.StoreIDv2(body.ID)
	if err != nil {
		mylog.Printf("ProcessChannelAdded: 映射频道ID失败 %s: %v", body.ID, err)
		return nil
	}

	// 储存 guild_id 关联（与 ProcessGuildNormalMessage 保持一致）
	idmap.WriteConfigv2(body.ID, "guild_id", body.GuildID)
	idmap.WriteConfigv2(body.ID, "type", "guild")

	// 映射创建者 user_id
	var operatorID64 int64
	if body.UserID != "" {
		operatorID64, err = idmap.StoreIDv2(body.UserID)
		if err != nil {
			mylog.Printf("ProcessChannelAdded: 映射用户ID失败 %s: %v", body.UserID, err)
			operatorID64 = 0
		}
	}

	mylog.Printf("频道[%s](%s)被创建，所属服务器[%s]，创建者[%s]", body.Name, body.ID, body.GuildID, body.UserID)

	// 构造 OneBot v11 group_increase notice 事件
	notice := GroupNoticeEvent{
		GroupID:    groupID64,
		NoticeType: "group_increase",
		OperatorID: operatorID64,
		PostType:   "notice",
		SelfID:     int64(p.BotID),
		SubType:    "invite", // bot 被"邀请"进入新群，语义最贴近频道创建
		Time:       time.Now().Unix(),
		UserID:     int64(p.BotID), // 表示 bot 自己加入了这个群
	}

	groupMsgMap := structToMap(notice)
	p.BroadcastMessageToAll(groupMsgMap)

	return nil
}

// ProcessChannelDeleted 处理 KOOK deleted_channel 事件，上报 OneBot group_decrease 通知
func (p *Processors) ProcessChannelDeleted(body *ChannelDeletedBody) error {
	// 跳过非文字频道（type!=1）
	if body.Type != 1 {
		mylog.Printf("ProcessChannelDeleted: 跳过非文字频道 %s (type=%d)", body.ID, body.Type)
		return nil
	}

	// 映射 KOOK channel ID 到 int64 group_id
	// 注意：频道已被删除，但 idmap 中可能还保留着映射关系
	groupID64, err := idmap.StoreIDv2(body.ID)
	if err != nil {
		mylog.Printf("ProcessChannelDeleted: 映射频道ID失败 %s: %v", body.ID, err)
		return nil
	}

	// 时间戳转换：KOOK deleted_at 为毫秒级，OneBot 使用秒级
	var eventTime int64
	if body.DeletedAt > 0 {
		eventTime = body.DeletedAt / 1000
	} else {
		eventTime = time.Now().Unix()
	}

	mylog.Printf("频道[%s]被删除", body.ID)

	// 确定 sub_type：默认使用 "kick"（标准 OneBot v11）
	// 如果配置了 use_disband_for_channel_delete，则使用 "disband"（NapCatQQ 扩展）
	subType := "kick"
	if config.GetDisbandForChannelDelete() {
		subType = "disband"
	}

	// 构造 OneBot v11 group_decrease notice 事件
	notice := GroupNoticeEvent{
		GroupID:    groupID64,
		NoticeType: "group_decrease",
		OperatorID: 0, // KOOK deleted_channel 事件不提供操作者
		PostType:   "notice",
		SelfID:     int64(p.BotID),
		SubType:    subType,
		Time:       eventTime,
		UserID:     int64(p.BotID), // 表示 bot 自己离开了这个群
	}

	groupMsgMap := structToMap(notice)
	p.BroadcastMessageToAll(groupMsgMap)

	return nil
}
