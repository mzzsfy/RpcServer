function rpcClient(wsURL) {
    if (!wsURL) {
        throw new Error('wsURL can not be empty!!')
    }
    this.wsURL = wsURL
    this.socket = {}
    this.handlers = {
        "_execjs": async param => (await new Function(param).call(this)) || "没有返回值"
    }
    this.connect()
    setInterval(()=>this.socket.send("[]"),25*1000)
}

WebSocket.prototype.send1 = function (o) {
    this.waiting.push(o)
    if (this.waiting.length >= 100) {
        let waiting = this.waiting
        this.waiting = []
        this.send("[" + waiting.join(",") + "]")
    }
}

rpcClient.prototype.connect = function () {
    console.log('begin of connect to wsURL: ' + this.wsURL);
    var _this = this;
    try {
        let socket = new WebSocket(this.wsURL);
        socket.waiting = []
        this.socket = socket;
        let number;
        number = setInterval(() => {
            if (socket.readyState > 1) {
                clearInterval(number)
                return
            }
            if (socket.waiting.length === 0) {
                return;
            }
            let waiting = socket.waiting
            socket.waiting = []
            socket.send("[" + waiting.join(",") + "]")
        }, 5);

        async function doSend(t, arr) {
            for (const data of arr) {
                try {
                    let handler = _this.handlers[data.action];
                    if (!handler) {
                        throw "没有这样的action:" + data.action
                    }
                    t.send1(JSON.stringify({
                        id: data.id,
                        data: await handler.call(_this, data.param)
                    }))
                } catch (e) {
                    t.send1(JSON.stringify({
                        id: data.id,
                        status: 1,
                        msg: e.toString(),
                    }))
                }
            }
        }

        this.socket.onmessage = async function (e) {
            let t = this;
            let arr = JSON.parse(e.data);
            while (arr.length > 10) {
                let arr1 = arr.slice(0, 10)
                setTimeout(async () => {
                    await doSend(t, arr1)
                }, 0)
                arr = arr.slice(10)
            }
            await doSend(t, arr)
        }
    } catch (e) {
        console.log("connection failed,reconnect after 3s");
        setTimeout(function () {
            _this.connect()
        }, 3000)
    }
    this.socket.onclose = function () {
        console.log("connection failed,reconnect after 3s");
        setTimeout(function () {
            _this.connect()
        }, 3000)
    }
};

rpcClient.prototype.regAction = function (func_name, func) {
    if (typeof func_name !== 'string') {
        throw new Error("an func_name must be string");
    }
    if (typeof func !== 'function') {
        throw new Error("must be function");
    }
    console.log("register func_name: " + func_name);
    this.handlers[func_name] = func;
    return this
}

// async function sleep(time) {
//     return new Promise(resolve => setTimeout(resolve, time))
// }
//
// demo=new rpcClient("ws://127.0.0.1:18880/ws?group=test")
//     .regAction('sleep', async p => ((await sleep(100)),p + "#" + Math.random()))
//     .regAction('test', p => {
//     return p + "#" + Math.random();
// })
// http://127.0.0.1:18880/call?group=test&name=*&action=test&param=123