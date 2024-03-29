import {default as PaneTree, init as panesInit} from "./panes.js";

export var init = async function() {
    await Promise.all([
        panesInit,
        window.aceLoaded,
    ]);

    var paneroot = document.createElement('div');
    document.body.appendChild(paneroot);
    var panetree = new PaneTree(paneroot);

    var editorroot = document.createElement('div');
    editorroot.id = "editor";
    panetree.insert(paneroot, "leftward", editorroot);
    editorroot.style.height = "100%";
    var editor = ace.edit("editor")
    editor.setTheme("ace/theme/terminal");

    var outputsection = document.createElement('div');
    panetree.insert(paneroot, "rightward", outputsection);

    var compilebutton = document.createElement('button');
    compilebutton.innerText = "Скомпилировать";
    outputsection.appendChild(compilebutton);

    compilebutton.addEventListener('click', () => { compile(editor); });
}();

async function compile(editor) {
    var asy_source = editor.getValue();
    var socket = new WebSocket("ws://" + document.location.host + "/asy", ["asyonline.asy"]);
    var values = [], antivalues = [], error = null;
    socket.onmessage = function(event) {
        if (antivalues.length > 0) {
            let [resolve, reject] = antivalues.shift();
            resolve(event.data);
            return;
        }
        values.push([event.data, null]);
    }
    socket.onerror = function(event) {
        error = new Error("websocket error");
        if (antivalues.length > 0) {
            let [resolve, reject] = antivalues.shift();
            reject(error);
            return;
        }
        values.push([null, error]);
    }
    socket.onclose = function(event) {
        error = new Error("websocket closed");
        if (antivalues.length > 0) {
            let [resolve, reject] = antivalues.shift();
            reject(error);
            return;
        }
        values.push([null, error]);
    }
    async function next_value() {
        if (values == null)
            throw error;
        if (values.length > 0) {
            let [value, error] = values.shift();
            if (error != null) {
                values = null;
                throw error;
            }
            return value;
        }
        return await new Promise( (resolve, reject) => {
            antivalues.push([resolve, reject]);
        });
    }
    await new Promise((resolve, reject) => {
        socket.onopen = resolve;
        antivalues.push([resolve, reject]);
    }).then(() => {
        antivalues.pop();
    })
    socket.send("add " + JSON.stringify({filename: "main.asy"}));
    socket.send(new Blob([asy_source]));
    socket.send("options " + JSON.stringify({
        format: "svg",
        verbosity: 3,
        stderrRedir: true,
        duration: 3.0 }));
    socket.send("start " + JSON.stringify({main: "main.asy"}));
    while (true) {
        console.log(await next_value());
    }
    //socket.close();
}

