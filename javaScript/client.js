function rpcClient(wsURL) {
    if (!wsURL) {
        throw new Error('wsURL can not be empty!!')
    }
    this.wsURL = wsURL
    this.socket = {}
    this.handlers = {
        "_execjs": param => new Function(param).call(this) || "没有返回值"
    }
    this.connect()
}

rpcClient.prototype.connect = function () {
    console.log('begin of connect to wsURL: ' + this.wsURL);
    var _this = this;
    try {
        this.socket = new WebSocket(this.wsURL);
        this.socket.onmessage = function (e) {
            let data = JSON.parse(e.data)
            try {
                let handler = _this.handlers[data.action];
                if (!handler) {
                    throw "没有这样的action:" + data.action
                }
                this.send(JSON.stringify({
                    id: data.id,
                    data: handler.call(_this, data.param)
                }))
            } catch (e) {
                this.send(JSON.stringify({
                    id: data.id,
                    status: 1,
                    msg: e.toString(),
                }))
            }
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
    return true
}

new rpcClient("ws://127.0.0.1:18880/ws?group=test").regAction('test', p => p+"#"+Math.random());
// http://127.0.0.1:18880/call?group=test&name=*&action=test&param=123
