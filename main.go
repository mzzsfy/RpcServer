package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	allWs all
	id    int32
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

func (a *all) loadByGroup(groupName string) *member {
	if load, ok := a.data.Load(groupName); ok {
		g := load.(*group)
		var r *member
		g.members.Range(func(key, value interface{}) bool {
			r = value.(*member)
			return false
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
	m.messages.Store(message.Id, message)
	m.sender <- message
}

func (m *member) over(message *Message) {
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
		for {
			select {
			case message := <-m.sender:
				err := conn.WriteJSON(message)
				if err != nil && message.callBack != nil {
					message.callBack <- &Result{
						Status: 1,
						Msg:    err.Error(),
					}
					continue
				}
			case <-over:
				break
			}
		}
	}()
	go func() {
		for {
			v := &Result{}
			err := conn.ReadJSON(&v)
			if err != nil {
				fmt.Println("ws连接错误:", err)
				over <- err.Error()
				allWs.del(groupName, memberName)
				break
			}
			if v.Id != "" {
				if load, ok := messages.Load(v.Id); ok {
					m := load.(*Message)
					if m.callBack != nil {
						m.callBack <- v
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
	var load *member
	if name == "*" {
		load = allWs.loadByGroup(groupName)
	} else {
		load = allWs.load(groupName, name)
	}
	if load == nil {
		fmt.Println("没有找到指定连接", groupName, name)
		marshal, _ := json.Marshal(Result{
			Status: 1,
			Data:   nil,
			Msg:    "没有找到指定连接",
		})
		w.Write(marshal)
		return
	}
	callBack := make(chan *Result)
	i := generateId()
	fmt.Println(i, "call", groupName, name, action, param)
	message := &Message{
		Id:       i,
		Action:   action,
		Param:    param,
		callBack: callBack,
	}
	load.send(message)
	defer load.over(message)
	select {
	case r := <-callBack:
		marshal, err := json.Marshal(r)
		fmt.Println(i, "res", string(marshal))
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		w.Write(marshal)
	case _ = <-time.After(10 * time.Second):
		marshal, _ := json.Marshal(Result{
			Status: 1,
			Data:   nil,
			Msg:    "超时",
		})
		fmt.Println(i, "res", "超时")
		w.Write(marshal)
	}
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
				Status string `json:"status"`
			}{"ok"}
			return true
		})
		return true
	})
	bytes, err := json.Marshal(&v)
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}
	w.Write(bytes)
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
		println("启动服务错误", err)
		return
	}
}
