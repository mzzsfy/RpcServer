package main

import (
    "encoding/json"
    "fmt"
    "github.com/gorilla/websocket"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
    "net/http"
    "os"
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
    log         *zap.Logger
    callToken   = os.Getenv("TOKEN")
    wsToken     = env("WS_TOKEN", callToken)
    selectToken = env("SELECT_TOKEN", callToken)
)

func init() {
    onNewAll(&allWs)
}

func env(name, defaultValue string) string {
    val, b := os.LookupEnv(name)
    if !b {
        return defaultValue
    }
    return val
}

func doSend(conn *websocket.Conn, w *[]*Message) {
    conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
    err := conn.WriteJSON(w)
    if err != nil {
        log.Info("发送失败", zap.Error(err))
        e := err.Error()
        for _, m := range *w {
            m.callBack <- NewResult(1, nil, e)
        }
    }
    *w = []*Message{}
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
        log.Info("ws未命名,分配名称:" + name)
    }
    if "true" == query.Get("randomSuffix") {
        name = name + "__" + generateId()
    }
    if wsToken != query.Get("token") {
        log.Info("token错误", zap.Any("token", query.Get("token")))
        result := NewResult(1, nil, "token错误")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        w.Write(marshal)
        return
    }
    up := &websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool { return true },
    }
    conn, err := up.Upgrade(w, r, nil)
    if err != nil {
        log.Info("websocket err:", zap.Error(err))
        w.Write([]byte(fmt.Sprint("websocket err:", err)))
        return
    }
    log.Info(groupName, zap.Any(name, "连接成功"))
    allWs.saveWs(groupName, name, conn, r)
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
    doExec(r, w, name, groupName, action, param)
}

func doExec(r *http.Request, w http.ResponseWriter, name string, groupName string, action string, param string) {
    start := time.Now()
    w.Header().Set("content-type", "application/json; charset=utf-8")
    query := r.URL.Query()
    if callToken != query.Get("token") {
        log.Info("token错误", zap.Any("token", query.Get("token")))
        result := NewResult(1, nil, "token错误")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        w.Write(marshal)
        return
    }
    var m *member
    if strings.Contains(name, "*") {
        m = allWs.loadLike(groupName, name)
    } else {
        m = allWs.load(groupName, name)
    }
    if m == nil {
        log.Info("没有找到指定连接", zap.Any(groupName, name))
        result := NewResult(1, nil, "没有找到指定连接")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        w.Write(marshal)
        return
    }
    callBack := make(chan *Result, 1)
    i := generateId()
    message := messagePool.Get().(*Message)
    defer messagePool.Put(message)
    message.Id = i
    message.Action = action
    message.Param = param
    message.callBack = callBack
    m.send(message)
    defer m.sendOver(message)
    select {
    case r := <-callBack:
        defer resultPool.Put(r)
        marshal, err := json.Marshal(r)
        log.Info("请求结束", zap.Any("id", i), zap.Any(groupName, m.name), zap.Any("call", action+"->"+param), zap.Any("time", time.Now().UnixMilli()-start.UnixMilli()), zap.Any("res", string(marshal)))
        if err != nil {
            w.WriteHeader(500)
            w.Write([]byte(err.Error()))
            return
        }
        if r.Status != 0 {
            w.WriteHeader(500)
        } else {
            onSuccess(m)
        }
        w.Write(marshal)
    case _ = <-time.After(10 * time.Second):
        result := NewResult(1, nil, "超时")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        log.Info("请求超时", zap.Any("id", i), zap.Any(groupName, m.name), zap.Any("call", action+"->"+param), zap.String("res", "超时"))
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
    doExec(r, w, name, groupName, "__exec", code)
}

func list(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query()
    if selectToken != query.Get("token") {
        log.Info("token错误", zap.Any("token", query.Get("token")))
        result := NewResult(1, nil, "token错误")
        defer resultPool.Put(result)
        marshal, _ := json.Marshal(result)
        w.Write(marshal)
        return
    }
    v := make(map[string]map[string]interface{})
    allWs.groups.Range(func(key, value interface{}) bool {
        g := value.(*group)
        g.members.Range(func(key, value interface{}) bool {
            m := value.(*member)
            m2 := v[m.groupName]
            if m2 == nil {
                m2 = make(map[string]interface{})
                v[m.groupName] = m2
            }
            m2[m.name] = struct {
                SuccessNum int32       `json:"successNum"`
                Start      string      `json:"start"`
                ConnTime   string      `json:"connTime"`
                Info       interface{} `json:"info"`
            }{*m.data[successNum].(*int32),
                m.start.Format(time.RFC3339),
                time.Now().Sub(m.start).String(),
                m.info,
            }
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
    log.Info("注册路径:" + pattern)
    http.HandleFunc(pattern, handler)
}

//todo:初始化后服务器下发指令
func main() {
    //f, _ := os.OpenFile("cpu.pprof", os.O_CREATE|os.O_RDWR, 0644)
    //e := pprof.StartCPUProfile(f)
    //if e != nil {
    //    println("性能分析启动错误", e)
    //}
    //go func() {
    //    time.Sleep(60 * time.Second)
    //    println("写入分析中")
    //    pprof.StopCPUProfile()
    //    println("写入分析完成,请执行 go tool pprof -http=:8888 cpu.pprof")
    //    os.Exit(0)
    //}()
    regHandle("/", index)
    regHandle("/exec", exec)
    regHandle("/call", call)
    regHandle("/ws", wsIndex)
    regHandle("/list", list)
    regHandle("/dash", dash)
    log.Info("启动服务,端口:18880")
    log.Info("当前token设置", zap.Any("token", callToken), zap.Any("wsToken", wsToken))
    err := http.ListenAndServe(":18880", nil)
    if err != nil {
        log.Error("启动服务错误", zap.Error(err))
        return
    }
}

func init() {
    config := zap.NewProductionEncoderConfig()
    config.CallerKey = ""
    config.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
    encoder := zapcore.NewConsoleEncoder(config)
    log = zap.New(
        zapcore.NewCore(encoder, newAsyncConsole(), zap.NewAtomicLevel()),
    )
}

func newAsyncConsole() *asyncConsole {
    cache := make(chan string, 2000)
    go func() {
        str := ""
        for {
            select {
            case b := <-cache:
                str += b
                if len(str) > 3000 {
                    os.Stdout.WriteString(str)
                    str = ""
                }
                i := len(cache) - 10
                if i > 200 {
                    str += "待写入日志过多,丢弃" + fmt.Sprint(i) + "条\n"
                    for j := 0; j < i; j++ {
                        <-cache
                    }
                }
            case <-time.After(10 * time.Millisecond):
                if str == "" {
                    continue
                }
                os.Stdout.WriteString(str)
                str = ""
            }
        }
    }()
    return &asyncConsole{
        cache,
    }
}

type asyncConsole struct {
    cache chan string
}

func (c asyncConsole) Write(p []byte) (n int, err error) {
    c.cache <- string(p)
    return len(p), nil
}

func (c asyncConsole) Sync() error {
    return nil
}