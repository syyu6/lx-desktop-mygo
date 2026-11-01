# Third-party notices

This app is licensed under the GNU Affero General Public License v3 (see
`LICENSE`). The parts of it that are built on other work are credited here.

## qq-music-api

`internal/qq` implements the QQ Music protocol after the behaviour and the
TypeScript implementation of [qq-music-api](https://github.com/Rain120/qq-music-api):
its musicu and legacy CGI transport, its catalogue, playlist and chart reads,
its ptlogin QR and OAuth login, its playback, and its signed-in library.

MIT License

Copyright (c) 2019

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## lx-music-desktop

Source scripts, and the rules the app checks their answers by, follow
[lx-music-desktop](https://github.com/lyswhut/lx-music-desktop) (Apache-2.0).
The app ships none of them: a listener imports their own.

## NeteaseCloudMusicApi

The eapi encryption and the login flow of `internal/netease` are written from
[NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)
(MIT).
