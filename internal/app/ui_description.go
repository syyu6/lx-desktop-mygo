package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// A collection's description is prose, and a header is no place for a page
// of it: it shows two lines, and the rest is read in a card that opens over
// the page as the pointer rests on them. The card waits a moment, so that
// passing over the header does not open it, and stays while the pointer is
// inside it or moving between it and the lines.

const (
	descriptionDelay = 500 * time.Millisecond
	descriptionGrace = 180 * time.Millisecond
)

// descriptionHover is what the card of a description knows between frames.
type descriptionHover struct {
	text string
	font ui.Font
	// width is the width the preview is laid out at, and overflow whether
	// the text takes more than the two lines it shows there: only then is
	// there anything the card would add.
	width         float32
	overflow      bool
	open          bool
	dismissed     bool // closed while the pointer was still on it
	entered, left time.Time
	popupWidth    float32
	popupHeight   float32
}

// collectionDescription shows a description as two lines, opening the whole
// of it in a card under the pointer.
func (a *app) collectionDescription(c *ui.Context, description string) {
	description = strings.TrimSpace(strings.ReplaceAll(description, "\r\n", "\n"))
	if description == "" {
		return
	}
	p := a.pal
	// Newlines fold into spaces: two lines of prose read as one passage,
	// rather than as the ragged right a writer's line breaks leave.
	preview := strings.ReplaceAll(description, "\n", " ")
	anchor := ui.Text(c.Key("description"), preview).
		FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45).Margin(2, 0, 0, 0).
		Label("简介").Description(description)
	s := ui.Local(anchor, "hover", func() descriptionHover { return descriptionHover{} })
	hovered := anchor.Hovered()
	width := anchor.LayoutBounds().W
	if width <= 0 {
		c.Invalidate()
		return
	}
	font := ui.Font{Family: c.Theme().Font, Size: 12.5, LineHeight: 1.45, Lang: c.Locale()}
	// The preview's height is only known once the text is shaped: a summary
	// that already fits in two lines has nothing the card would add.
	if s.text != description || s.width != width || s.font != font {
		_, height := ui.ShapeText(preview, font, width).Size()
		*s = descriptionHover{text: description, font: font, width: width, overflow: height > 2*font.Size*font.LineHeight+0.5}
	}
	if !s.overflow {
		return
	}
	anchor.Focusable()
	focused := anchor.FocusVisible()
	now := c.Now()
	if hovered || focused {
		if s.entered.IsZero() {
			s.entered = now
		}
		if !s.dismissed {
			if wait := descriptionDelay - now.Sub(s.entered); wait > 0 && !focused {
				c.After(wait)
			} else {
				s.open = true
			}
		}
	} else {
		s.entered = time.Time{}
	}
	wasOpen := s.open
	pop := popoverColors(p.dark)
	panel := ui.PopoverBase(c.Key("description-card"), anchor, &s.open, func(panel ui.Element) {
		w, h := c.Size()
		cardWidth := min(float32(480), w-32)
		if s.popupWidth != cardWidth {
			s.popupWidth = cardWidth
			_, s.popupHeight = ui.ShapeText(description,
				ui.Font{Family: font.Family, Size: 13, LineHeight: 1.6, Lang: font.Lang}, cardWidth-32).Size()
		}
		panel.Width(cardWidth).Padding(16).Radius(12).Margin(8, 0, 0, 0).
			Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 28, -6, cardShadow(p.dark)).Label("完整简介")
		ui.Scroll(c.Key("description-scroll")).Height(min(s.popupHeight, min(float32(320), h*0.45)-32)).
			Focusable().Label("简介内容").Children(func() {
			ui.Text(c, description).FontSize(13).TextColor(p.text).LineHeight(1.6).Selectable()
		})
	})
	// Closing the card under the pointer leaves it closed until the pointer
	// leaves the preview: it is a dismiss, not the pointer moving away.
	if wasOpen && !s.open {
		s.dismissed = true
	}
	panelHovered, panelFocused := panel.Hovered(), panel.FocusWithin()
	if hovered || focused || panelHovered || panelFocused {
		s.left = time.Time{}
		return
	}
	s.dismissed = false
	if s.open {
		if s.left.IsZero() {
			s.left = now
		}
		if wait := descriptionGrace - now.Sub(s.left); wait > 0 {
			c.After(wait)
		} else {
			s.open = false
			c.Invalidate()
		}
	}
}

// cardShadow is the shade a card over the page is drawn with.
func cardShadow(dark bool) ui.Color {
	if dark {
		return ui.RGBA(0, 0, 0, 0.5)
	}
	return ui.RGBA(0, 0, 0, 0.18)
}
