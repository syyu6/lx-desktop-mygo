package qq

import (
	"encoding/json"
	"errors"
	"html"
	"strconv"
	"sync"

	"netease-music-mygo/internal/music"
)

// QQ's comments, read a page at a time.
//
// Its latest comments page by a cursor QQ hands back — the sequence number
// of the last one read — rather than by an offset, so the cursor of each
// page is kept here, keyed by the song and the offset it leads to. A cursor
// that has gone, as one does when the app restarts or another account signs
// in, says so: the app opens the comments again rather than show the first
// page as if it were the second.

var errNoComments = errors.New("qq: no comments in the answer")

// commentCursor is where a page of latest comments starts.
type commentCursor struct {
	seq, version string
	page, total  int
}

var commentCursors = struct {
	sync.Mutex
	pages map[string]map[int]commentCursor
}{pages: map[string]map[int]commentCursor{}}

// commentCursorFor is where the page at offset starts: the first page for
// offset 0, and nil for a later one whose cursor has gone.
func commentCursorFor(thread string, offset int) *commentCursor {
	if offset == 0 {
		return &commentCursor{}
	}
	commentCursors.Lock()
	defer commentCursors.Unlock()
	if c, ok := commentCursors.pages[thread][offset]; ok {
		return &c
	}
	return nil
}

// rememberCursor keeps where the page at offset starts, for the next one.
func rememberCursor(thread string, offset int, cursor *commentCursor) {
	commentCursors.Lock()
	defer commentCursors.Unlock()
	if commentCursors.pages[thread] == nil {
		commentCursors.pages[thread] = map[int]commentCursor{}
	}
	commentCursors.pages[thread][offset] = *cursor
}

// commentPage is a page of QQ's comments: its latest, or its hot ones.
type commentPage struct {
	CommentList *struct {
		Comments []object        `json:"Comments"`
		Total    int             `json:"Total"`
		HasMore  json.RawMessage `json:"HasMore"`
	} `json:"CommentList"`
	TotalVer string `json:"TotalVer"`
}

// readComments reads one page of a song's comments: its latest, or its hot
// ones, which have no pages after the first.
func readComments(id int64, limit int, cursor *commentCursor, hot bool) (out []music.Comment, total int, more bool, next *commentCursor, err error) {
	if cursor == nil {
		cursor = &commentCursor{}
	}
	param := map[string]any{
		"BizType": 1, "BizId": strconv.FormatInt(id, 10), "PageSize": limit,
		"PageNum": cursor.page, "LastCommentSeqNo": cursor.seq, "PicEnable": 1,
	}
	method := "GetNewCommentList"
	if hot {
		method = "GetHotCommentList"
		param["HotType"], param["WithAirborne"] = 1, 0
	} else {
		param["FromCommentId"], param["WithHot"], param["LastTotal"] = "", 0, cursor.total
		param["LastTotalVer"] = textOr(cursor.version, "0")
	}
	var data commentPage
	err = rpc("music.globalComment.CommentRead", method, param, &data)
	if err != nil {
		return nil, 0, false, nil, err
	}
	if data.CommentList == nil || data.CommentList.Comments == nil {
		return nil, 0, false, nil, errNoComments
	}
	out = decodeComments(data.CommentList.Comments)
	total = data.CommentList.Total
	if hot {
		return out, total, false, nil, nil
	}
	if entries := data.CommentList.Comments; len(entries) > 0 {
		seq := text(entries[len(entries)-1], "SeqNo")
		if seq != "" && hasMore(data.CommentList.HasMore) {
			more = true
			next = &commentCursor{seq: seq, version: data.TotalVer, page: cursor.page + 1, total: total}
		}
	}
	return out, total, more, next, nil
}

// hasMore reads the flag QQ gives as a number or as a string, depending on
// the endpoint.
func hasMore(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "1" || s == "true"
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n != 0
	}
	return false
}

// decodeComments reads a page of comments into what the pane shows. QQ's
// ids are strings of digits; one that is not a number is hashed, so that a
// comment still has an id to be keyed by.
func decodeComments(entries []object) []music.Comment {
	out := make([]music.Comment, 0, len(entries))
	for _, entry := range entries {
		remote := text(entry, "CmId")
		if remote == "" {
			continue
		}
		id := number(entry, "CmId")
		if id == 0 {
			id = hashID(remote)
		}
		c := music.Comment{
			ID:         id,
			Content:    html.UnescapeString(text(entry, "Content")),
			Time:       number(entry, "PubTime") * 1000,
			LikedCount: int(number(entry, "PraiseNum")),
		}
		c.User.Nickname = text(entry, "Nick")
		c.User.AvatarURL = text(entry, "Avatar")
		out = append(out, c)
	}
	return out
}

// hashID gives a comment whose id is not a number one, stable across
// reads: FNV-1a, with the sign bit masked off.
func hashID(s string) int64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return int64(h & 0x7fffffffffffffff)
}
