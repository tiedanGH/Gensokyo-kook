package Processor

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hoshinonyaruko/gensokyo-kook/mylog"
	"github.com/idodo/golang-bot/kaihela/api/base/event"
)

const (
	strictDedupeTTL   = 10 * time.Minute
	fallbackDedupeTTL = 2 * time.Second
)

type inboundDedupeStore struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func newInboundDedupeStore() *inboundDedupeStore {
	return &inboundDedupeStore{
		items: make(map[string]time.Time),
	}
}

var globalInboundDedupe = newInboundDedupeStore()

func (d *inboundDedupeStore) hitOrStore(key string, ttl time.Duration, now time.Time) (bool, time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for k, exp := range d.items {
		if now.After(exp) {
			delete(d.items, k)
		}
	}

	if exp, ok := d.items[key]; ok && now.Before(exp) {
		remaining := exp.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		return true, ttl - remaining
	}

	d.items[key] = now.Add(ttl)
	return false, 0
}

func contentSummary(content string, maxLen int) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// shouldDropInboundEvent 会在 Processor 入口进行兜底去重。
func shouldDropInboundEvent(scene string, data *event.MessageKMarkdownEvent) bool {
	channelType := strings.ToUpper(strings.TrimSpace(data.ChannelType))
	if channelType == "" {
		channelType = strings.ToUpper(strings.TrimSpace(scene))
	}
	msgID := strings.TrimSpace(data.MsgId)
	targetID := strings.TrimSpace(data.TargetId)
	authorID := strings.TrimSpace(data.AuthorId)
	if data.Author.ID != "" {
		authorID = strings.TrimSpace(data.Author.ID)
	}
	nonce := strings.TrimSpace(data.Nonce)
	content := strings.TrimSpace(data.Content)

	now := time.Now()
	if msgID != "" {
		key := fmt.Sprintf("strict:%s:%s", channelType, msgID)
		hit, age := globalInboundDedupe.hitOrStore(key, strictDedupeTTL, now)
		mylog.Printf("[dedupe][processor] scene=%s layer=strict_msg_id key=%s hit=%v source_msg_id=%s nonce=%s channel_type=%s target_id=%s author_id=%s age_ms=%d content=%q",
			scene, key, hit, msgID, nonce, channelType, targetID, authorID, age.Milliseconds(), contentSummary(content, 80))
		if hit {
			mylog.Printf("[dedupe][processor][warn] Drop duplicated KOOK inbound event by strict msg_id. key=%s msg_id=%s nonce=%s channel_type=%s target_id=%s author_id=%s",
				key, msgID, nonce, channelType, targetID, authorID)
			return true
		}
		return false
	}

	if nonce != "" {
		key := fmt.Sprintf("fallback_nonce:%s:%s:%s:%s", channelType, targetID, authorID, nonce)
		hit, age := globalInboundDedupe.hitOrStore(key, fallbackDedupeTTL, now)
		mylog.Printf("[dedupe][processor] scene=%s layer=fallback_nonce key=%s hit=%v source_msg_id=%s nonce=%s channel_type=%s target_id=%s author_id=%s age_ms=%d content=%q",
			scene, key, hit, msgID, nonce, channelType, targetID, authorID, age.Milliseconds(), contentSummary(content, 80))
		if hit {
			mylog.Printf("[dedupe][processor][warn] Drop duplicated KOOK inbound event by fallback nonce. This may misjudge real repeated speech. key=%s channel_type=%s target_id=%s author_id=%s nonce=%s age_ms=%d",
				key, channelType, targetID, authorID, nonce, age.Milliseconds())
			return true
		}
		return false
	}

	// 最后兜底：msg_id 和 nonce 都没有时，使用短窗口组合键并明确告警可能误判。
	if content != "" && data.MsgTimestamp > 0 {
		key := fmt.Sprintf("fallback_content:%s:%s:%s:%d:%s", channelType, targetID, authorID, data.MsgTimestamp, content)
		hit, age := globalInboundDedupe.hitOrStore(key, fallbackDedupeTTL, now)
		mylog.Printf("[dedupe][processor] scene=%s layer=fallback_content_ts key=%s hit=%v source_msg_id=%s nonce=%s channel_type=%s target_id=%s author_id=%s age_ms=%d content=%q",
			scene, key, hit, msgID, nonce, channelType, targetID, authorID, age.Milliseconds(), contentSummary(content, 80))
		if hit {
			mylog.Printf("[dedupe][processor][warn] Drop duplicated KOOK inbound event by fallback content+timestamp. This may misjudge real repeated speech. key=%s channel_type=%s target_id=%s author_id=%s age_ms=%d",
				key, channelType, targetID, authorID, age.Milliseconds())
			return true
		}
	}

	return false
}
