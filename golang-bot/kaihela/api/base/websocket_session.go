package base

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gorilla/websocket"
	"github.com/idodo/golang-bot/kaihela/api/helper"
	log "github.com/sirupsen/logrus"
)

type WebSocketSession struct {
	*StateSession
	Token        string
	BaseUrl      string
	SessionFile  string
	WsConn       *websocket.Conn
	WsWriteLock  *sync.Mutex
	ConnMu       sync.RWMutex
	ReconnectMu  sync.Mutex
	Reconnecting bool
	SessionCtx   context.Context
	SessionStop  context.CancelFunc
	SessionSeq   uint64
	//sWSClient
}

type GateWayHttpApiResult struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Url string `json:"url"`
	} `json:"data"`
}

func NewWebSocketSession(token, baseUrl, sessionFile, gateWay string, compressed int) *WebSocketSession {
	s := &WebSocketSession{
		Token: token, BaseUrl: baseUrl, SessionFile: sessionFile}
	if content, err := os.ReadFile(sessionFile); err == nil && len(content) > 0 {
		data := make([]interface{}, 0)
		err := sonic.Unmarshal(content, &data)
		if err != nil {
			if len(data) == 2 {
				s.SessionId = data[0].(string)
				s.MaxSn = data[0].(int64)
			}
		} else {
			log.WithError(err).Error("unmarsal from sessionFile error", sessionFile)
		}

	}
	s.StateSession = NewStateSession(gateWay, compressed)
	s.NetworkProxy = s
	s.WsWriteLock = new(sync.Mutex)
	return s
}

func (ws *WebSocketSession) ReqGateWay() (string, error) {
	client := helper.NewApiHelper("/v3/gateway/index", ws.Token, ws.BaseUrl, "", "")
	client.SetQuery(map[string]string{"compress": strconv.Itoa(ws.Compressed)})
	data, err := client.Get()
	if err != nil {
		log.WithError(err).Error("ReqGateWay")
		return "", err
	}
	result := &GateWayHttpApiResult{}
	err = sonic.Unmarshal(data, result)
	if err != nil {
		log.WithError(err).Error("ReqGateWay")
		return "", err
	}
	if result.Code == 0 && len(result.Data.Url) > 0 {
		return result.Data.Url, nil
	}
	log.WithField("result", result).Error("ReqGateWay resultCode is not 0 or Url is empty")
	return "", errors.New("resultCode is not 0 or Url is empty")

}
func (ws *WebSocketSession) ConnectWebsocket(gateway string) error {
	if ws.SessionId != "" {
		gateway += "&" + fmt.Sprintf("sn=%d&sessionId=%s&resume=1", ws.MaxSn, ws.SessionId)
	}
	log.WithField("gateway", gateway).Info("ConnectWebsocket")

	c, resp, err := websocket.DefaultDialer.Dial(gateway, nil)
	log.Infof("webscoket dial resp:%+v", resp)
	if err != nil {
		log.WithError(err).Error("ConnectWebsocket Dial")
		return err
	}
	sessionID := ws.bindConnection(c)
	log.WithField("sessionSeq", sessionID).Info("kook websocket connection bound to session lifecycle")

	ws.wsConnectOk()
	ws.startReadLoop(c, sessionID)
	return nil
}

func (ws *WebSocketSession) bindConnection(conn *websocket.Conn) uint64 {
	ws.ConnMu.Lock()
	defer ws.ConnMu.Unlock()

	if ws.SessionStop != nil {
		log.Info("cancel previous websocket session lifecycle before binding new connection")
		ws.SessionStop()
	}
	if ws.WsConn != nil && ws.WsConn != conn {
		log.Info("close previous websocket connection before binding new connection")
		_ = ws.WsConn.Close()
	}

	ws.WsConn = conn
	ws.SessionCtx, ws.SessionStop = context.WithCancel(context.Background())
	ws.SessionSeq = atomic.AddUint64(&ws.SessionSeq, 1)
	return ws.SessionSeq
}

func (ws *WebSocketSession) startReadLoop(conn *websocket.Conn, sessionSeq uint64) {
	log.WithField("sessionSeq", sessionSeq).Info("read loop started")
	go func() {
		defer log.WithField("sessionSeq", sessionSeq).Info("read loop exited")
		for {
			if ws.isSessionInactive(conn, sessionSeq) {
				log.WithField("sessionSeq", sessionSeq).Info("read loop exits because session is inactive")
				return
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				log.WithError(err).WithField("sessionSeq", sessionSeq).Warn("read loop exits due to websocket read error")
				if ws.isSessionInactive(conn, sessionSeq) {
					log.WithField("sessionSeq", sessionSeq).Info("handleDisconnect skipped because read loop belongs to stale session")
					return
				}
				ws.handleDisconnect("read_loop", err)
				return
			}
			log.WithField("message", message).WithField("sessionSeq", sessionSeq).Trace("websocket recv")
			_, err = ws.ReceiveData(message)
			if err != nil {
				log.WithError(err).WithField("sessionSeq", sessionSeq).Error("ReceiveData error")
			}
		}
	}()
}

func (ws *WebSocketSession) isSessionInactive(conn *websocket.Conn, sessionSeq uint64) bool {
	ws.ConnMu.RLock()
	defer ws.ConnMu.RUnlock()

	if ws.WsConn != conn {
		return true
	}
	if ws.SessionSeq != sessionSeq {
		return true
	}
	if ws.SessionCtx == nil {
		return false
	}

	select {
	case <-ws.SessionCtx.Done():
		return true
	default:
		return false
	}
}

func (ws *WebSocketSession) handleDisconnect(source string, err error) {
	log.WithError(err).WithField("source", source).Info("enter handleDisconnect")
	ws.ReconnectMu.Lock()
	if ws.Reconnecting {
		log.WithField("source", source).Info("handleDisconnect skipped because reconnect loop is already running")
		ws.ReconnectMu.Unlock()
		return
	}
	ws.Reconnecting = true
	log.WithField("source", source).Info("set reconnecting=true")
	ws.ReconnectMu.Unlock()

	ws.shutdownCurrentSession("disconnect_"+source, true)
	go ws.reconnectLoop(source, err)
}

func (ws *WebSocketSession) shutdownCurrentSession(reason string, closeConn bool) {
	log.WithFields(log.Fields{
		"reason":    reason,
		"closeConn": closeConn,
	}).Info("shutdown websocket session lifecycle")

	ws.stopHeartbeatLifecycle(reason)

	ws.ConnMu.Lock()
	defer ws.ConnMu.Unlock()

	if ws.SessionStop != nil {
		log.WithField("reason", reason).Info("cancel websocket session context")
		ws.SessionStop()
		ws.SessionStop = nil
		ws.SessionCtx = nil
	}
	if closeConn && ws.WsConn != nil {
		log.WithField("reason", reason).Info("close websocket connection")
		_ = ws.WsConn.Close()
		ws.WsConn = nil
	}
}

func (ws *WebSocketSession) reconnectLoop(source string, lastErr error) {
	log.WithFields(log.Fields{
		"source": source,
		"error":  lastErr,
	}).Warn("reconnect loop started")
	defer func() {
		ws.ReconnectMu.Lock()
		ws.Reconnecting = false
		log.Info("set reconnecting=false")
		ws.ReconnectMu.Unlock()
		log.Info("reconnect loop exited")
	}()

	for {
		for attempt := 1; attempt <= 5; attempt++ {
			ws.ConnMu.RLock()
			hasOldConn := ws.WsConn != nil
			ws.ConnMu.RUnlock()
			log.WithFields(log.Fields{
				"attempt":    attempt,
				"maxAttempt": 5,
				"oldConnNil": !hasOldConn,
			}).Info("kook websocket reconnect attempt")

			gateway, err := ws.ReqGateWay()
			if err != nil {
				log.WithError(err).Warn("kook websocket reconnect failed to get gateway")
				time.Sleep(5 * time.Second)
				continue
			}

			ws.GateWay = gateway
			ws.FSM.SetState(StatusGateway)
			err = ws.ConnectWebsocket(gateway)
			if err == nil {
				log.Info("kook websocket reconnect dial success, read loop and ping loop will be recreated by new session lifecycle")
				return
			}

			log.WithError(err).Warn("kook websocket reconnect dial failed")
			time.Sleep(5 * time.Second)
		}

		log.Warn("kook websocket reconnect 5 attempts failed, sleep 60 seconds before next round")
		time.Sleep(60 * time.Second)
	}
}

func (ws *WebSocketSession) SendData(data []byte) error {
	ws.WsWriteLock.Lock()
	defer ws.WsWriteLock.Unlock()

	ws.ConnMu.RLock()
	conn := ws.WsConn
	ws.ConnMu.RUnlock()
	if conn == nil {
		err := errors.New("websocket connection is nil")
		log.WithError(err).Warn("SendData aborted")
		return err
	}

	err := conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.WithError(err).Warn("SendData failed, trigger disconnect handling")
		ws.handleDisconnect("send_data", err)
		return err
	}
	return nil
}

func (ws *WebSocketSession) SaveSessionId(sessionId string) error {
	dataArray := []interface{}{sessionId, ws.MaxSn}
	data, err := sonic.Marshal(dataArray)
	if err != nil {
		log.WithError(err).Error("SaveSessionId")
		return err
	}
	err = os.WriteFile(ws.SessionFile, data, 0644)
	if err != nil {
		log.WithError(err).Error("SaveSessionId")
		return err
	}
	return nil
}

func (ws *WebSocketSession) Start() {
	ws.StateSession.Start()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	for {
		select {

		case <-interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			ws.ConnMu.RLock()
			conn := ws.WsConn
			ws.ConnMu.RUnlock()
			if conn != nil {
				err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					log.Println("write close:", err)
					return
				}
			}
			ws.shutdownCurrentSession("interrupt", true)
			ws.ReconnectMu.Lock()
			if ws.Reconnecting {
				log.Info("interrupt clears reconnecting flag")
				ws.Reconnecting = false
			}
			ws.ReconnectMu.Unlock()
			if conn == nil {
				return
			}
			return
		}
	}
}
