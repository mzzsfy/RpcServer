package main

import (
    "encoding/json"
    "go.uber.org/zap"
    "net/http"
    "sync/atomic"
    "time"
)

var (
    sendNum          = new(bool) //总次数
    successNum       = new(bool) //成功次数
    lastSecondNum    = new(bool) //上一秒发送的次数
    lastSecondRecord = new(bool) //上一秒发送的总次数的记录
    lastMinuteRecord = new(bool) //上一分钟发送的次数
    lastMinuteNum    = new(bool) //上一分钟发送的总次数的记录
    lastHourRecord   = new(bool) //上一小时发送的次数
    lastHourNum      = new(bool) //上一小时发送的总次数的记录
    lastDayRecord    = new(bool) //上一天发送的次数
    lastDayNum       = new(bool) //上一天发送的总次数的记录
    over             = new(bool) //结束
)

func onNewMember(o *member) *member {
    o.data = make(map[*bool]interface{})
    o.data[sendNum] = new(int32)
    o.data[successNum] = new(int32)
    o.data[lastSecondNum] = new(int32)
    o.data[lastSecondRecord] = new(int32)
    o.data[lastMinuteNum] = new(int32)
    o.data[lastMinuteRecord] = new(int32)
    o.data[lastHourNum] = new(int32)
    o.data[lastHourRecord] = new(int32)
    o.data[lastDayNum] = new(int32)
    o.data[lastDayRecord] = new(int32)
    c := make(chan string)
    o.data[over] = c
    go func() {
        ticker := time.NewTicker(time.Second)
        tick := ticker.C
        for {
            select {
            case t := <-tick:
                nowSend := *o.data[sendNum].(*int32)
                record := *o.data[lastSecondRecord].(*int32)
                num := nowSend - record
                *o.data[lastSecondNum].(*int32) = num
                *o.data[lastSecondRecord].(*int32) = nowSend
                //统计分钟
                if t.Second() == 0 {
                    record := *o.data[lastMinuteRecord].(*int32)
                    num := nowSend - record
                    *o.data[lastMinuteNum].(*int32) = num
                    *o.data[lastMinuteRecord].(*int32) = nowSend
                }
                //统计小时
                if t.Second() == 0 && t.Minute() == 0 {
                    record := *o.data[lastHourRecord].(*int32)
                    num := nowSend - record
                    *o.data[lastHourNum].(*int32) = num
                    *o.data[lastHourRecord].(*int32) = nowSend
                }
                //统计天
                if t.Second() == 0 && t.Minute() == 0 && t.Hour() == 0 {
                    record := *o.data[lastDayRecord].(*int32)
                    num := nowSend - record
                    *o.data[lastDayNum].(*int32) = num
                    *o.data[lastDayRecord].(*int32) = nowSend
                }
            case <-c:
                ticker.Stop()
                return
            }
        }
    }()
    return o
}

func onNewGroup(o *group) *group {
    o.data = make(map[*bool]interface{})
    return o
}

func onNewAll(o *all) *all {
    o.data = make(map[*bool]interface{})
    return o
}

func onSuccess(m *member) {
    atomic.AddInt32(m.data[successNum].(*int32), 1)
}

func onSendOver(m *member) int32 {
    return atomic.AddInt32(&m.waiting, -1)
}

func onSend(m *member) {
    atomic.AddInt32(&m.waiting, 1)
    atomic.AddInt32(m.data[sendNum].(*int32), 1)
}

func onRemoveMember(o *member) {
    o.data[over].(chan string) <- "over"
}

func onRemoveGroup(o *group) {
    //todo
}

func dash(w http.ResponseWriter, r *http.Request) {
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
    ai := initInfoMap()
    allWs.groups.Range(func(key, value interface{}) bool {
        g := value.(*group)
        gm := make(map[string]interface{})
        v[key.(string)] = gm
        gi := initInfoMap()
        g.members.Range(func(key, value interface{}) bool {
            mi := initInfoMap()
            gm[key.(string)] = mi
            m := value.(*member)
            addInfo(m, mi, gi, ai)
            mi["info"] = m.info
            return true
        })
        gm["__all__"] = gi
        return true
    })
    v["__all__"] = ai
    b, err := json.Marshal(&v)
    if err != nil {
        w.Write([]byte(err.Error()))
        return
    }
    w.Header().Set("content-type", "application/json; charset=utf-8")
    w.Write(b)
}

func initInfoMap() map[string]interface{} {
    m := make(map[string]interface{})
    m["sendNum"] = int32(0)
    m["successNum"] = int32(0)
    m["failNum"] = int32(0)
    m["lastSecondNum"] = int32(0)
    m["lastMinuteNum"] = int32(0)
    m["lastHourNum"] = int32(0)
    m["lastDayNum"] = int32(0)
    //m["lastSecondRecord"] = int32(0)
    //m["lastMinuteRecord"] = int32(0)
    //m["lastHourRecord"] = int32(0)
    //m["lastDayRecord"] = int32(0)
    return m
}
func addInfo(m *member, ms ...map[string]interface{}) {
    for _, i := range ms {
        doAddInfo(m, i)
    }
}
func doAddInfo(m *member, i map[string]interface{}) {
    i["sendNum"] = i["sendNum"].(int32) + *m.data[sendNum].(*int32)
    i["successNum"] = i["successNum"].(int32) + *m.data[successNum].(*int32)
    i["failNum"] = i["failNum"].(int32) + (i["sendNum"].(int32) - i["successNum"].(int32) - m.waiting)
    i["lastSecondNum"] = i["lastSecondNum"].(int32) + *m.data[lastSecondNum].(*int32)
    i["lastMinuteNum"] = i["lastMinuteNum"].(int32) + *m.data[lastMinuteNum].(*int32)
    i["lastHourNum"] = i["lastHourNum"].(int32) + *m.data[lastHourNum].(*int32)
    i["lastDayNum"] = i["lastDayNum"].(int32) + *m.data[lastDayNum].(*int32)

    //i["lastSecondRecord"] = i["lastSecondRecord"].(int32) + *m.data[lastSecondRecord].(*int32)
    //i["lastMinuteRecord"] = i["lastMinuteRecord"].(int32) + *m.data[lastMinuteRecord].(*int32)
    //i["lastHourRecord"] = i["lastHourRecord"].(int32) + *m.data[lastHourRecord].(*int32)
    //i["lastDayRecord"] = i["lastDayRecord"].(int32) + *m.data[lastDayRecord].(*int32)
}