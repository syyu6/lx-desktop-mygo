/*!
 * @name 逃逸测试音源
 * @description 集成测试用：它试图逃出沙箱
 * @version 1.0
 */

// Everything here must fail. The test checks that the app is still
// running and that nothing was read or written on the computer.

const { EVENT_NAMES, send } = globalThis.lx;
const results = {};

function tryIt(name, fn) {
  try {
    results[name] = String(fn());
  } catch (e) {
    results[name] = 'BLOCKED: ' + ((e && e.message) || e);
  }
}

// The globals the engine leaves out.
tryIt('std', () => typeof std);
tryIt('os', () => typeof os);
tryIt('print', () => typeof print);
tryIt('process', () => typeof process);
tryIt('require', () => typeof require);

// The filesystem through the modules a runtime could load. The engine sets
// no module loader at all, so an import is refused rather than resolved.
function blocked(name, module) {
  const was = results[name];
  results[name] = was ? was + ' ' + module : module;
}

let stdModule = null;
import('qjs:std').then((m) => {
  stdModule = m;
  tryIt('loadFile', () => {
    const s = m.loadFile('/etc/hostname');
    return s === null ? 'NULL' : 'READ ' + s;
  });
  tryIt('writeFile', () => {
    const f = m.open('/tmp/lxsource-escape.txt', 'w');
    if (!f) return 'NULL';
    f.puts('escaped');
    f.close();
    return 'WROTE';
  });
  tryIt('loadSelf', () => {
    const s = m.loadFile('simple.js');
    return s === null ? 'NULL' : 'READ';
  });
}, (e) => {
  blocked('loadFile', 'BLOCKED: ' + ((e && e.message) || e));
  blocked('writeFile', 'BLOCKED');
  blocked('loadSelf', 'BLOCKED');
});

// Leaving the app, if os were reachable.
let osModule = null;
import('qjs:os').then((m) => {
  osModule = m;
  tryIt('osExit', () => {
    m.exit(1);
    return 'EXITED';
  });
}, (e) => {
  blocked('osExit', 'BLOCKED: ' + ((e && e.message) || e));
});

send(EVENT_NAMES.inited, {
  sources: {
    wy: { name: '网易云音乐', type: 'music', actions: ['musicUrl'], qualitys: ['320k'] },
  },
});

globalThis.__escape = () => JSON.stringify(results);
