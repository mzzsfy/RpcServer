package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "github.com/gorilla/websocket"
    "net/http"
    "strings"
    "sync"
    "sync/atomic"
    "time"
)

var (
    allWs       all
    id          int32
    messagePool = sync.Pool{New: func() interface{} { return &Message{} }}
    resultPool  = sync.Pool{New: func() interface{} { return &Result{} }}
)

type all struct {
    data sync.Map
}

func (a *all) saveWs(groupName, memberName string, conn *websocket.Conn) {
    store, _ := a.data.LoadOrStore(groupName, &group{
        members: sync.Map{},
        name:    groupName,
    })
    g := store.(*group)
    if _, ok := g.members.Load(memberName); ok {
        fmt.Println("替换旧的连接,group", groupName, "name", memberName)
    }
    g.members.Store(memberName, NewMember(groupName, memberName, conn))
}

func (a *all) load(groupName, memberName string) *member {
    if load, ok := a.data.Load(groupName); ok {
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
        return strings.HasSuffix(text, rule[2:])
    }
    return rule == text
}

func (a *all) loadLike(groupName, memberName string) *member {
    if load, ok := a.data.Load(groupName); ok {
        g := load.(*group)
        var r *member
        g.members.Range(func(key, value interface{}) bool {
            if !match(memberName, key.(string)) {
                return true
            }
            m := value.(*member)
            if m.waiting <= 3 {
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
    if load, ok := a.data.Load(groupName); ok {
        g := load.(*group)
        g.members.Delete(memberName)
        d := true
        g.members.Range(func(key, value interface{}) bool {
            d = false
            return false
        })
        if d {
            a.data.Delete(groupName)
        }
    }
}

type group struct {
    members sync.Map
    name    string
}

type member struct {
    conn      *websocket.Conn
    name      string
    groupName string
    waiting   int32
    sendNum   int32
    messages  sync.Map
    sender    chan *Message
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

func (m *member) send(message *Message) {
    atomic.AddInt32(&m.waiting, 1)
    atomic.AddInt32(&m.sendNum, 1)
    m.messages.Store(message.Id, message)
    m.sender <- message
}

func (m *member) over(message *Message) {
    atomic.AddInt32(&m.waiting, -1)
    m.messages.Delete(message.Id)
}

func NewMember(groupName, memberName string, conn *websocket.Conn) *member {
    m := &member{
        conn:      conn,
        name:      memberName,
        groupName: groupName,
        sender:    make(chan *Message),
        messages:  sync.Map{},
    }
    messages := &m.messages
    over := make(chan string)
    go func() {
        var w []*Message
        for {
            select {
            case <-time.After(10 * time.Millisecond):
                if len(w) == 0 {
                    continue
                }
                err := conn.WriteJSON(w)
                w = []*Message{}
                if err != nil {
                    fmt.Println("发送失败", err)
                }
            case message := <-m.sender:
                w = append(w, message)
                if len(w) >= 100 {
                    err := conn.WriteJSON(w)
                    w = []*Message{}
                    if err != nil {
                        fmt.Println("发送失败", err)
                    }
                }
            case <-over:
                break
            }
        }
    }()
    go func() {
        for {
            var arr []*Result
            _, b, err := conn.ReadMessage()
            count := bytes.Count(b, []byte("{\"id\":\""))
            if err != nil {
                fmt.Println("ws连接错误:", err)
                over <- err.Error()
                allWs.del(groupName, memberName)
                break
            }
            for i := 0; i < count; i++ {
                result := NewResult(0, nil, "")
                arr = append(arr, result)
            }
            err = json.Unmarshal(b, &arr)
            if err != nil {
                fmt.Println("json解析错误:", err)
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
        }
    }()
    return m
}

func index(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("一个测试服务"))
}

func wsIndex(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query()
    groupName, name := query.Get("group"), query.Get("name")
    if groupName == "" {
        w.WriteHeader(400)
        w.Write([]byte("缺少必须参数 group: " + groupName))
        return
    }
    if name == "" {
        name = "_generate_" + generateId()
        fmt.Println("ws未命名,分配名称", name)
    }
    up := &websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool { return true },
    }
    conn, err := up.Upgrade(w, r, nil)
    if err != nil {
        fmt.Println("websocket err:", err)
        w.Write([]byte(fmt.Sprint("websocket err:", err)))
        return
    }
    fmt.Println(groupName, name, "连接成功")
    allWs.saveWs(groupName, name, conn)
}

func generateId() string {
    return fmt.Sprint(atomic.AddInt32(&id, 1))
}

func call(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query()
    groupName, name := query.Get("group"), query.Get("name")
    if groupName == "" || name == "" {
        w.WriteHeader(400)
        w.Write([]byte("缺少必须参数 group: " + groupName + ",或name: " + name))
        return
    }
    action, param := query.Get("action"), query.Get("param")
    if action == "" {
        w.WriteHeader(400)
        w.Write([]byte("缺少必须参数 action: " + action))
        return
    }
    doExec(w, name, groupName, action, param)
}

func doExec(w http.ResponseWriter, name string, groupName string, action string, param string) {
    w.Header().Set("content-type", "application/json; charset=utf-8")
    var load *member
    if strings.Contains(name, "*") {
        load = allWs.loadLike(groupName, name)
    } else {
        load = allWs.load(groupName, name)
    }
    if load == nil {
        fmt.Println("没有找到指定连接", groupName, name)
        result := NewResult(1, nil, "没有找到指定连接")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        w.Write(marshal)
        return
    }
    callBack := make(chan *Result)
    i := generateId()
    fmt.Println(i, "call", groupName, name, action, param)
    message := messagePool.Get().(*Message)
    defer messagePool.Put(message)
    message.Id = i
    message.Action = action
    message.Param = param
    message.callBack = callBack
    load.send(message)
    defer load.over(message)
    select {
    case r := <-callBack:
        defer resultPool.Put(r)
        marshal, err := json.Marshal(r)
        fmt.Println(i, "res", string(marshal))
        if err != nil {
            w.WriteHeader(500)
            w.Write([]byte(err.Error()))
            return
        }
        w.Write(marshal)
    case _ = <-time.After(10 * time.Second):
        result := NewResult(1, nil, "超时")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        fmt.Println(i, "res", "超时")
        w.WriteHeader(500)
        w.Write(marshal)
    }
}

func NewResult(Status int, Data interface{}, Msg string) *Result {
    result := resultPool.Get().(*Result)
    result.Status = Status
    result.Data = Data
    result.Msg = Msg
    return result
}

func exec(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query()
    groupName, name := query.Get("group"), query.Get("name")
    if groupName == "" || name == "" {
        w.WriteHeader(400)
        w.Write([]byte("缺少必须参数 group: " + groupName + ",或name: " + name))
        return
    }
    code := query.Get("code")
    doExec(w, name, groupName, "_execjs", code)
}

func list(w http.ResponseWriter, r *http.Request) {
    v := make(map[string]map[string]interface{})
    allWs.data.Range(func(key, value interface{}) bool {
        g := value.(*group)
        g.members.Range(func(key, value interface{}) bool {
            m := value.(*member)
            m2 := v[m.groupName]
            if m2 == nil {
                m2 = make(map[string]interface{})
                v[m.groupName] = m2
            }
            m2[m.name] = struct {
                Status     string `json:"status"`
                SendNumber int32  `json:"sendNumber"`
                Waiting    int32  `json:"waiting"`
            }{"ok", m.sendNum, m.waiting}
            return true
        })
        return true
    })
    b, err := json.Marshal(&v)
    if err != nil {
        w.Write([]byte(err.Error()))
        return
    }
    w.Header().Set("content-type", "application/json; charset=utf-8")
    w.Write(b)
}
func regHandle(pattern string, handler func(http.ResponseWriter, *http.Request)) {
    fmt.Println("注册路径:", pattern)
    http.HandleFunc(pattern, handler)
}

//todo:初始化后服务器下发指令
func main() {
    regHandle("/", index)
    regHandle("/exec", exec)
    regHandle("/call", call)
    regHandle("/ws", wsIndex)
    regHandle("/list", list)
    println("启动服务,端口:18880")
    err := http.ListenAndServe(":18880", nil)
    if err != nil {
        println("启动服务错误", err.Error())
        return
    }
}