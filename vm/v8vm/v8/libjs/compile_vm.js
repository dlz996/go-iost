'use strict';

function NativeModule(id) {
    this.filename = id + '.js';
    this.id = id;
    this.exports = {};
    this.loaded = false;
}

NativeModule._cache = {};

NativeModule.require = function (id) {
    if (id == '_native_module') {
        return NativeModule;
    }

    var cached = NativeModule.getCached(id);
    if (cached) {
        return cached.exports;
    }

    var nativeModule = new NativeModule(id);
    nativeModule.compile();
    nativeModule.cache();

    return nativeModule.exports;
};

NativeModule.getCached = function(id) {
    return NativeModule._cache[id];
};

NativeModule.getSource = function(id) {
    return _native_require(id);
};

NativeModule.wrap = function(script) {
    return NativeModule.wrapper[0] + script + NativeModule.wrapper[1];
};

NativeModule.wrapper = [
    '(function (exports, require, module, __filename, __dirname) {\n',
    '\n});'
];

NativeModule.prototype.compile = function () {
    var source = NativeModule.getSource(this.id);
    source = NativeModule.wrap(source);

    var fn = _native_run(source, this.filename);
    fn(this.exports, NativeModule.require, this, this.filename);

    this.loaded = true;
};

NativeModule.prototype.cache = function() {
    NativeModule._cache[this.id] = this;
};

var require = NativeModule.require;

var injectGas = require('inject_gas');

var _IOSTInstruction_counter = new IOSTInstruction;