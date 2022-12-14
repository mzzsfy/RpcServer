package main

import (
    "encoding/json"
    "go.uber.org/zap"
    "net/http"
    "sync"
    "sync/atomic"
    "time"
)

var (
    dieWs            []*member
    dieWsLock        = sync.Mutex{}
    sendNum          = new(bool) //总次数
    successNum       = new(bool) //成功次数
    lastSecondNum    = new(bool) //上一秒发送的次数
    lastMinuteNum    = new(bool) //上一分钟发送的总次数的记录
    lastHourNum      = new(bool) //上一小时发送的总次数的记录
    lastDayNum       = new(bool) //上一天发送的总次数的记录
    lastSecondRecord = new(bool) //上一秒发送的总次数的记录
    lastMinuteRecord = new(bool) //上一分钟发送的次数
    lastHourRecord   = new(bool) //上一小时发送的次数
    lastDayRecord    = new(bool) //上一天发送的次数
    over             = new(bool) //结束
)

func onNewMember(o *member) *member {
    o.data = initDataMap()
    c := make(chan string)
    o.data[over] = c
    go func() {
        ticker := time.NewTicker(time.Second)
        tick := ticker.C
        for {
            select {
            case t := <-tick:
                doRecord(o, t)
            case <-c:
                ticker.Stop()
                return
            }
        }
    }()
    return o
}

func onNewGroup(o *group) *group {
    o.data = initDataMap()
    return o
}

func onNewAll(o *all) *all {
    o.data = initDataMap()
    return o
}

func initDataMap() map[*bool]interface{} {
    m := make(map[*bool]interface{})
    m[sendNum] = new(int32)
    m[successNum] = new(int32)
    m[lastSecondNum] = new(int32)
    m[lastSecondRecord] = new(int32)
    m[lastMinuteNum] = new(int32)
    m[lastMinuteRecord] = new(int32)
    m[lastHourNum] = new(int32)
    m[lastHourRecord] = new(int32)
    m[lastDayNum] = new(int32)
    m[lastDayRecord] = new(int32)
    return m
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
    err := o.conn.Close()
    if err != nil {
        log.Error("关闭连接错误", zap.String(o.groupName, o.name), zap.Error(err))
    }
    o.end = new(time.Time)
    *o.end = time.Now()
    o.data[over].(chan string) <- "over"
    dieWsLock.Lock()
    defer dieWsLock.Unlock()
    dieWs = append(dieWs, o)
    if len(dieWs) > 20 {
        dieWs = dieWs[1:]
    }
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
    fillInfoMap(ai, allWs.data)
    allWs.groups.Range(func(key, value interface{}) bool {
        g := value.(*group)
        gm := make(map[string]interface{})
        v[key.(string)] = gm
        gi := initInfoMap()
        fillInfoMap(gi, g.data)
        g.members.Range(func(key, value interface{}) bool {
            mi := initInfoMap()
            gm[key.(string)] = mi
            m := value.(*member)
            addInfo(m, mi)
            mi["info"] = m.info
            mi["waiting"] = m.waiting
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

func dieLast(w http.ResponseWriter, r *http.Request) {
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
    for _, m := range dieWs {
        mg := v[m.groupName]
        if mg == nil {
            mg = make(map[string]interface{})
            v[m.groupName] = mg
        }
        mi := initInfoMap()
        if mg[m.name] != nil {
            mg[m.name+"__rename_"+generateId()] = mi
        } else {
            mg[m.name] = mi
        }
        addInfo(m, mi)
        mi["info"] = m.info
        mi["start"] = m.start.Format(time.RFC3339)
        mi["connTime"] = m.end.Sub(m.start).String()
    }
    b, err := json.Marshal(&v)
    if err != nil {
        w.Write([]byte(err.Error()))
        return
    }
    w.Header().Set("content-type", "application/json; charset=utf-8")
    w.Write(b)
}

func fillInfoMap(m map[string]interface{}, i map[*bool]interface{}) {
    send := *i[sendNum].(*int32)
    m["send"] = i[sendNum]
    m["success"] = send
    m["fail"] = send - *i[successNum].(*int32)

    m["second"] = send - *i[lastSecondNum].(*int32)
    m["minute"] = send - *i[lastMinuteNum].(*int32)
    m["hour"] = send - *i[lastHourNum].(*int32)
    m["day"] = send - *i[lastDayNum].(*int32)

    m["lastSecond"] = i[lastSecondNum]
    m["lastMinute"] = i[lastMinuteNum]
    m["lastHour"] = i[lastHourNum]
    m["lastDay"] = i[lastDayNum]
}
func initInfoMap() map[string]interface{} {
    m := make(map[string]interface{})
    m["send"] = int32(0)
    m["success"] = int32(0)
    m["fail"] = int32(0)

    m["second"] = int32(0)
    m["minute"] = int32(0)
    m["hour"] = int32(0)
    m["day"] = int32(0)

    m["lastSecond"] = int32(0)
    m["lastMinute"] = int32(0)
    m["lastHour"] = int32(0)
    m["lastDay"] = int32(0)
    return m
}
func addInfo(m *member, ms ...map[string]interface{}) {
    for _, i := range ms {
        doAddInfo(m, i)
    }
}
func doAddInfo(m *member, i map[string]interface{}) {
    i["send"] = i["send"].(int32) + *m.data[sendNum].(*int32)
    i["success"] = i["success"].(int32) + *m.data[successNum].(*int32)
    i["fail"] = i["fail"].(int32) + (i["send"].(int32) - i["success"].(int32) - m.waiting)

    i["second"] = i["second"].(int32) + (*m.data[sendNum].(*int32) - *m.data[lastSecondNum].(*int32))
    i["minute"] = i["minute"].(int32) + (*m.data[sendNum].(*int32) - *m.data[lastMinuteNum].(*int32))
    i["hour"] = i["hour"].(int32) + (*m.data[sendNum].(*int32) - *m.data[lastHourNum].(*int32))
    i["day"] = i["day"].(int32) + (*m.data[sendNum].(*int32) - *m.data[lastDayNum].(*int32))

    i["lastSecond"] = i["lastSecond"].(int32) + *m.data[lastSecondNum].(*int32)
    i["lastMinute"] = i["lastMinute"].(int32) + *m.data[lastMinuteNum].(*int32)
    i["lastHour"] = i["lastHour"].(int32) + *m.data[lastHourNum].(*int32)
    i["lastDay"] = i["lastDay"].(int32) + *m.data[lastDayNum].(*int32)
}

func doRecord(o *member, t time.Time) {
    nowSend := *o.data[sendNum].(*int32)
    record := *o.data[lastSecondRecord].(*int32)
    num := nowSend - record

    *o.data[lastSecondNum].(*int32) = num
    *o.data[lastSecondRecord].(*int32) = nowSend
    //将信息向上传递
    groupOnSecondDoRecord(o, num)
    //统计分钟
    if t.Second() == 0 {
        record := *o.data[lastMinuteRecord].(*int32)
        num := nowSend - record
        *o.data[lastMinuteNum].(*int32) = num
        *o.data[lastMinuteRecord].(*int32) = nowSend
        groupOnMinuteDoRecord(o, num)
    }
    //统计小时
    if t.Second() == 0 && t.Minute() == 0 {
        record := *o.data[lastHourRecord].(*int32)
        num := nowSend - record
        *o.data[lastHourNum].(*int32) = num
        *o.data[lastHourRecord].(*int32) = nowSend
        groupOnHourDoRecord(o, num)
    }
    //统计天
    if t.Second() == 0 && t.Minute() == 0 && t.Hour() == 0 {
        record := *o.data[lastDayRecord].(*int32)
        num := nowSend - record
        *o.data[lastDayNum].(*int32) = num
        *o.data[lastDayRecord].(*int32) = nowSend
        groupOnDayDoRecord(o, num)
    }
}

func groupOnSecondDoRecord(m *member, num int32) {
    if v, ok := allWs.groups.Load(m.groupName); ok {
        g := v.(*group)
        *g.data[lastSecondNum].(*int32) = num
        //*g.data[lastSecondRecord].(*int32) = nowSend
    }
}

func groupOnMinuteDoRecord(m *member, num int32) {

}

func groupOnHourDoRecord(m *member, num int32) {

}

func groupOnDayDoRecord(m *member, num int32) {

}

func allOnSecondDoRecord(m *member, num int32) {

}

func allOnMinuteDoRecord(m *member, num int32) {

}

func allOnHourDoRecord(m *member, num int32) {

}

func allOnDayDoRecord(m *member, num int32) {

}