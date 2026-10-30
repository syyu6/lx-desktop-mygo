/*!
 * @name 测试音源
 * @description 集成测试用，不联网时也答得出
 * @version 1.0
 * @author mygo
 */

// A source of the shape the real ones have: it answers musicUrl at once,
// fetches for lyrics through lx.request, and counts what it was asked.
const { EVENT_NAMES, request, on, send } = globalThis.lx;

let asked = 0;

on(EVENT_NAMES.request, ({ source, action, info }) => {
  if (action === 'musicUrl') {
    asked++;
    if (info.musicInfo.name === 'Missing') {
      return Promise.reject(new Error('找不到这首歌'));
    }
    return `https://example.invalid/${source}/${info.musicInfo.id}/${info.type}.mp3`;
  }
  if (action === 'lyric') {
    // lx.request takes a callback and returns a cancel function, as the
    // real scripts use it.
    return new Promise((resolve) => {
      request('https://example.invalid/lyric', { method: 'GET' }, () => {
        resolve({ lyric: '[00:01.00]测试歌词' });
      });
    });
  }
  if (action === 'pic') {
    return 'https://example.invalid/cover.jpg';
  }
  return Promise.reject(new Error('不支持'));
});

send(EVENT_NAMES.inited, {
  sources: {
    wy: {
      name: '网易云音乐',
      type: 'music',
      actions: ['musicUrl', 'lyric', 'pic'],
      qualitys: ['128k', '320k', 'flac', 'flac24bit'],
    },
  },
});

// What the tests read to know the script ran in a sandbox.
globalThis.__asked = () => asked;
