package main

import (
    "bytes"
    "encoding/json"
    "github.com/gorilla/websocket"
    "go.uber.org/zap"
    "net/http"
    "strings"
    "sync"
    "time"
)

type all struct {
    groups sync.Map
    data   map[*bool]interface{}
}

type group struct {
    members sync.Map
    name    string
    data    map[*bool]interface{}
}

type member struct {
    conn      *websocket.Conn
    name      string
    groupName string
    start     time.Time
    end       *time.Time
    info      map[string]string
    messages  sync.Map
    sender    chan *Message
    waiting   int32
    data      map[*bool]interface{}
}

type Message struct {
    Id       string `json:"id"`
    Action   string `json:"action"`
    Param    string `json:"param"`
    callBack chan *Result
}

type Result struct {
    Id     string      `json:"id"`
    Status int         `json:"status"`
    Data   interface{} `json:"data"`
    Msg    string      `json:"msg"`
}

func (a *all) saveWs(groupName, memberName string, conn *websocket.Conn, r *http.Request) {
    store, _ := a.groups.LoadOrStore(groupName, onNewGroup(&group{
        members: sync.Map{},
        name:    groupName,
    }))
    g := store.(*group)
    if old, ok := g.members.Load(memberName); ok {
        onRemoveMember(old.(*member))
        log.Info("替换旧的连接,group", zap.String(groupName, memberName))
    }
    const clientInfo = "clientInfo"
    infoStr := r.URL.Query().Get(clientInfo)
    info := make(map[string]string)
    if infoStr != "" {
        if strings.HasPrefix(infoStr, "{") && strings.HasSuffix(infoStr, "}") {
            err := json.Unmarshal([]byte(infoStr), &info)
            if err != nil {
                info[clientInfo] = infoStr
            }
        } else {
            info[clientInfo] = infoStr
        }
    }
    g.members.Store(memberName, NewMember(groupName, memberName, conn, info))
}

func (a *all) load(groupName, memberName string) *member {
    if load, ok := a.groups.Load(groupName); ok {
        g := load.(*group)
        if value, o := g.members.Load(memberName); o {
            return value.(*member)
        }
    }
    return nil
}

func match(rule, text string) bool {
    if rule == "*" {
        return true
    } else if strings.HasSuffix(rule, "*") {
        return strings.HasPrefix(text, rule[:len(rule)-2])
    } else if strings.HasPrefix(rule, "*") {
        return strings.HasSuffix(text, rule[1:])
    }
    return rule == text
}

func (a *all) loadLike(groupName, memberName string) *member {
    if load, ok := a.groups.Load(groupName); ok {
        g := load.(*group)
        var r *member
        g.members.Range(func(key, value interface{}) bool {
            if !match(memberName, key.(string)) {
                return true
            }
            m := value.(*member)
            if m.waiting < 1 {
                r = m
                return false
            }
            if r == nil {
                r = m
            }
            if m.waiting < r.waiting {
                r = m
            }
            return true
        })
        return r
    }
    return nil
}

func (a *all) del(groupName, memberName string) {
    if load, ok := a.groups.Load(groupName); ok {
        g := load.(*group)
        if value, o := g.members.Load(memberName); o {
            onRemoveMember(value.(*member))
        }
        g.members.Delete(memberName)
        d := true
        g.members.Range(func(key, value interface{}) bool {
            d = false
            return false
        })
        if d {
            onRemoveGroup(g)
            a.groups.Delete(groupName)
        }
    }
}

func (m *member) send(message *Message) {
    onSend(m)
    m.messages.Store(message.Id, message)
    m.sender <- message
}

func (m *member) sendOver(message *Message) {
    onSendOver(m)
    m.messages.Delete(message.Id)
}

func NewMember(groupName, memberName string, conn *websocket.Conn, info map[string]string) *member {
    m := onNewMember(&member{
        conn:      conn,
        name:      memberName,
        groupName: groupName,
        start:     time.Now(),
        sender:    make(chan *Message, 1),
        messages:  sync.Map{},
        info:      info,
    })
    messages := &m.messages
    over := make(chan string)
    sendNow := make(chan []*Message)
    go func() {
        for {
            time.Sleep(20 * time.Second)
            select {
            case sendNow <- []*Message{}:
            case <-time.After(time.Second):
                return
            }
        }
    }()
    go func() {
        var w []*Message
        num := 5
        for {
            if num > 300 {
                num = 300
            }
            if num < 0 {
                num = 0
            }
            select {
            case <-time.After(10 * time.Millisecond):
                num--
                if len(w) == 0 {
                    continue
                }
                m.doSend(&w)
            case message := <-sendNow:
                m.doSend(&message)
            case message := <-m.sender:
                w = append(w, message)
                if len(w) >= num {
                    if len(m.sender) > 0 {
                        num++
                    }
                    m.doSend(&w)
                }
            case <-over:
                return
            }
        }
    }()
    go func() {
        for {
            var arr []*Result
            _, b, err := conn.ReadMessage()
            count := bytes.Count(b, []byte("{\"id\":\""))
            if err != nil {
                allWs.del(groupName, memberName)
                over <- "over"
                log.Info("读取错误:", zap.Error(err))
                return
            }
            for i := 0; i < count; i++ {
                result := NewResult(0, nil, "")
                arr = append(arr, result)
            }
            err = json.Unmarshal(b, &arr)
            if err != nil {
                log.Info("json解析错误:", zap.Error(err))
                for _, result := range arr {
                    resultPool.Put(result)
                }
                break
            }
            for _, v := range arr {
                if v.Id != "" {
                    if load, ok := messages.Load(v.Id); ok {
                        m := load.(*Message)
                        if m.callBack != nil {
                            m.callBack <- v
                        }
                    }
                }
            }
            if len(arr) == 0 {
                sendNow <- []*Message{}
            }
        }
    }()
    return m
}

func (m *member) doSend(w *[]*Message) {
    m.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
    err := m.conn.WriteJSON(w)
    if err != nil {
        log.Info("发送失败", zap.Error(err))
        //allWs.del(m.groupName, m.name)
        e := err.Error()
        for _, m := range *w {
            m.callBack <- NewResult(1, nil, e)
        }
    }
    *w = []*Message{}
}