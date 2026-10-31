package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/source"
	"netease-music-mygo/internal/subscription"
)

type pageKind int

const (
	pageHome pageKind = iota
	pageDaily
	pageCharts
	pagePlaylist
	pageSearch
	pageFM
	pageArtist
	pageAlbum
	pageUser
	pagePodcast
	pagePodcasts
	pageSonglists
)

type route struct {
	page pageKind
	// src is the source the page shows the thing of: a playlist's service.
	// Empty is NetEase's, as the app's own pages are.
	src string
	// id is the playlist, artist, album, user or podcast the page shows.
	// Every source numbers its playlists, so one id serves them all; src
	// says whose it is.
	id int64
	// query and kind are what a search page searched for, and only is the
	// search source it shows alone: empty for all of them together.
	query string
	kind  netease.SearchType
	only  string
}

// key names a playlist in the source it is from, as a page is routed to.
func playlistRoute(src string, id int64) route {
	return route{page: pagePlaylist, src: src, id: id}
}

// app is the state both windows show. Everything in it is read and changed
// on the main thread: by the views, and by goroutines through post.
type app struct {
	win   *mygo.Window
	notch *notch
	tray  *mygo.Tray // the icon in the menu bar or notification area, while it shows
	art   *artCache
	pl    *player.Player
	pal   palette
	// focused says whether the app is the active one, as the system told
	// it: while it is not, nothing the window shows moves on its own, and
	// the frames it would have asked for are left to the system, which
	// gives back what drawing them took.
	focused bool
	// shown says whether the window is on screen, whatever has the
	// keyboard: shown or minimized, as the system tells it, and never
	// hidden by the app. With focused it says whether anyone may be
	// looking at the window.
	//
	// It is kept here rather than asked of the window each frame: IsVisible
	// is a question to the window server, and a view asks this many times a
	// frame.
	shown bool

	route   route
	history []route
	forward []route // the pages gone back from, the last first to go to

	profile      *netease.Profile
	mine, saved  []netease.Playlist // the listener's playlists: made, and saved
	recPlaylists []netease.Playlist // the day's playlists picked for the listener
	login        loginState
	naming       nameDialog     // the name of a playlist asked for
	deleting     deleteDialog   // whether to delete a playlist, asked
	liked        map[int64]bool // the songs the listener likes
	toast        string         // a message to show once
	picked       pickedUp       // the song a login picked up, waiting to play

	// The episodes the listener likes, and doesn't, as far as NetEase has
	// answered, and those asked about: see ui_like.go.
	likedPrograms, askedPrograms map[int64]bool

	daily       []netease.Track
	dailySpin   spinState // the record on the daily card
	dailyBars   barState  // the bars on its play button
	featured    []netease.Playlist
	charts      []netease.Playlist
	homeErr     string
	homeLoading bool
	homeAt      time.Time // when what the home page shows was asked for

	playlists  map[string]*netease.Playlist // by playlistKey
	loadingPl  map[string]bool
	plErr      map[string]string
	artists    map[int64]*artistData
	artistTab  int // what an artist's page lists: artistHot, artistLatest, artistAlbums or artistEvents
	albums     map[int64]*albumData
	users      map[int64]*userData
	podcasts   map[int64]*podcastData
	podcastTab *podcastTab // the podcasts tab's lists
	trackList  ui.ListState
	// Where the page and the sidebar are scrolled, kept while Now Playing
	// covers them, unbuilt.
	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	mineCollapsed, savedCollapsed bool

	query       string
	searchBox   ui.Handle
	searches    map[route]*searchData // the results of each search, by its page
	searched    route                 // the search last shown, which the sidebar goes back to
	suggestions suggestState          // what the search field's query may be after

	nowPlaying  bool
	npOpen      bool      // Now Playing as it last showed: open or not
	npChangedAt time.Time // when it last opened or closed
	npPane      int       // paneLyrics, paneQueue, paneStage, paneComments, paneProgram or paneCaptions
	npPaneTop   float32   // how far the cover beside the pane starts below its top
	npTabsH     float32   // how tall the pane's tabs were, last laid out
	lyricAwake  bool      // the glide stops where the lyrics are, once drawn
	lyricScroll ui.ScrollState
	lyricGlide  lyricGlide
	lyricAt     int
	lyricTrack  int64
	queueList   ui.ListState
	queueAt     int // the row of the queue pane last scrolled to, -1 while it is hidden
	comments    commentState
	tabCount    rollState // the count on the comments' tab, as it rolls

	fmMode        int    // the FM mode chosen, in fmModes
	radioStarting string // the radio asked for its first songs, or ""

	scrub      float64
	scrubbing  bool
	volume     float64
	volumeOpen bool // the volume popover of the player bar shows
	mutedFrom  float64

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	npScrub               scrubState
	backdropCache         backdropCache // the backdrop of Now Playing, as last painted
	// pageBackdrop is the backdrop behind the pages, painted while the
	// appearance follows the player: the same colors as Now Playing's, of
	// its own, since both show at once on the frame Now Playing opens.
	pageBackdrop backdropCache
	// coverArt is the cover the window is painted with, and coverTintKnown
	// whether there is one: kept as it loads, so that a frame between two
	// songs keeps the last one's colors rather than flashing the system's.
	coverArt     *artwork
	npArt        *artwork     // the cover whose colors Now Playing shows
	npCoverState coverState   // the cover of Now Playing, as it is tilted and flicked
	wallState    wallState    // the wall of the queue's covers, as it is dragged
	quiet        quietState   // the quiet parts of the song playing, for its hot comments
	captions     captionState // the episode playing's captions, and the model that writes them
	sig          playerSig
	quitting     bool

	settings    settings
	settingsWin *mygo.Window
	cacheUsed   int64 // the bytes the song cache takes, -1 until measured
	updates     bool  // the update plugin is used (useUpdater)

	// The sources the listener imported (lx.go): the scripts kept, what
	// they say about themselves, and the resolver that runs them. srcErr
	// says why there are none, when they cannot be opened. srcState holds
	// what the resolver reads off the main thread.
	srcStore  *lxsource.Store
	src       *lxsource.Resolver
	srcState  *sourceState
	sources   []lxsource.Info
	srcErr    string
	importing importDialog
	// srcCaps is what each script says it plays, by ID, once its runtime
	// has said so: the settings tag a script with the platforms it covers,
	// which only a running script knows (lx.go). srcCapsAsked keeps the
	// scripts asked, so that one is started once, not on every frame.
	srcCaps      map[string][]lxsource.Source
	srcCapsErr   map[string]string
	srcCapsAsked map[string]bool
	// reg is the sources the app browses, NetEase and the services it
	// ships, each of which the listener may turn off (sources.go).
	reg *source.Registry
	// sourceExpanded says which built-in sources the settings show open,
	// their switches under their names: open until the listener closes one.
	sourceExpanded map[string]bool
	// settingsTab is the tab of the settings showing, 0 for 通用: the window
	// keeps it for as long as it is open.
	settingsTab int
	// removing is the script the settings are asking about deleting, and
	// srcUpdating the scripts being fetched again from their addresses: the
	// dialog, and the buttons that wait while it happens.
	removing    removeDialog
	srcUpdating map[string]bool
	// chain is the order the sources are tried in to play a song: the app's
	// own and the scripts', as the settings keep them (chain.go).
	chain *source.Chain
	// sideWidth is how wide the sidebar shows, which the listener drags its
	// edge to change; sideHidden whether it shows at all; sideRail whether
	// it shows folded to a rail of icons, which the fold at its foot
	// changes.
	sideWidth  float32
	sideHidden bool
	sideRail   bool
	// accountOpen says the listener's own menu is dropped up from their
	// picture at the foot of the sidebar.
	accountOpen bool
	// railFrame is sideRail as the frame being built sees it: the fold
	// takes effect on the next frame, so that everything laid out within
	// this one agrees on whether the rail is there.
	railFrame bool
	// barLeft is the room the window's controls take at the frame's left, as
	// the frame being built has it: the rail widens to clear them, so that
	// the traffic lights sit over it rather than over the line it shares
	// with the page.
	barLeft float32
	// tint is the color the playing song's cover gives the window while the
	// appearance follows the player: the cover's most vivid color, kept as
	// it loads, and whether there is one at all.
	tint      ui.Color
	tintKnown bool
	// themeFrom is the palette the window is painted with, and themeTo the
	// one the playing song asks for; themeAt is when it began going from
	// one to the other, and themeMix how far it has. A song's colors are
	// eased into the next one's, so that the window changes songs rather
	// than flashing between them.
	themeFrom palette
	// themeActive says the window is painted with the player's colors,
	// themeTint the song whose colors it is on the way to, and themeAt
	// when it began going there.
	themeActive bool
	themeTint   ui.Color
	themeAt     time.Time
	// seed is the song the session kept, which the window follows until
	// the player has it: the session is restored a moment after the window
	// opens, and the system's colors would show until then.
	seed *netease.Track
	// winColor is the window's own background, as last set: it follows the
	// playing song's while the appearance does, and is set only as it
	// changes.
	winColor string
	// sideDirty says the width was dragged and is not saved yet, and
	// sideSavedAt when it was last saved: a drag saves as it settles, not
	// on every frame.
	sideDirty   bool
	sideSavedAt time.Time
	// square is the playlist square the 歌单 page shows, and squarePick the
	// source whose square the listener chose (ui_songlists.go).
	square     *squareState
	squarePick string
	// subs are the playlists the listener follows from another service, and
	// subSyncing those being fetched again (ui_subscriptions.go).
	subs         *subscription.Store
	subSyncing   map[string]bool
	subscribeDlg subscribeDialog
	// subsCollapsed is whether the sidebar's followed playlists show.
	subsCollapsed bool

	restored     bool   // playback picked up where it was left
	savedSession string // the session as last kept
	sessionDue   bool   // a save of the session is due
}

// post runs fn on the main thread and redraws both windows. Without a
// window, as in tests, it drops fn.
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		fn()
		a.redraw()
	})
}

func (a *app) redraw() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
}

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

func (a *app) showMain() {
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
		// From the menu bar alone the app may be inactive.
		mygo.App.Focus()
	}
}

// Run runs the app until it quits.
func Run() error {
	a := &app{
		playlists:  map[string]*netease.Playlist{},
		loadingPl:  map[string]bool{},
		plErr:      map[string]string{},
		artists:    map[int64]*artistData{},
		albums:     map[int64]*albumData{},
		users:      map[int64]*userData{},
		podcasts:   map[int64]*podcastData{},
		podcastTab: &podcastTab{},
		searches:   map[route]*searchData{},
		lyricAt:    -1,
		queueAt:    -1,
		cacheUsed:  -1,
		focused:    true,
		shown:      true,
	}
	moveOldData()
	a.useUpdater()
	mygo.App.WhenReady(func() {
		a.settings = loadSettings()
		a.settings.applyAppearance()
		// The session is back a moment after the window opens, and the
		// player with it: until then, the window follows the song the
		// session kept, so that it does not show the system's colors first.
		a.seed = a.seedTrack()
		a.seedTint()
		a.sideWidth, a.sideHidden, a.sideRail = a.settings.Sidebar, a.settings.SidebarHidden, a.settings.SidebarRail
		a.npPane = a.lyricsPaneChosen()
		// Before the window: on Windows it lays out its content as it is
		// made, and the view asks the player what plays.
		a.art = newArtCache(a.post)
		a.pl = player.New(a.post)
		a.pl.Quality = func() string { return a.settings.Quality }
		a.pl.Normalize = func() bool { return a.settings.Normalize }
		a.pl.Prefetch = func() bool { return a.settings.Prefetch }
		a.pl.Cache = newSongCache(a.settings.CacheMB)
		// The listener's own sources are read before the player can ask for
		// a song: a script's runtime starts when it is first asked.
		a.loadSources()
		a.loadSourceList()
		a.keepSweeping()
		a.loadRegistry()
		// The followed playlists are read as the window opens, their songs
		// at hand, and fetched again once the pages can show them.
		a.loadFollowed()
		a.pl.Changed = func() {
			a.keepSession()
			a.syncCaptions()
		}
		a.volume = a.settings.Volume
		a.pl.SetVolume(a.volume)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         mygo.App.Name(),
			Width:         1180,
			Height:        780,
			MinWidth:      880,
			MinHeight:     580,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// As tall as the edge over a page (Linux, Windows).
			TitleBarHeight: buttonsHeight,
			// Inset from the corner, within the sidebar, as macOS 26's
			// apps with a sidebar place them.
			TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			Content:              ui.View(a.view),
		})
		a.setMenu()
		a.boot()
		// The playlists the listener follows are fetched again, those kept up
		// to date: their copies showed already.
		a.autoSyncSubscriptions()
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(a.syncDock)
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(a.freshenHome)
		// Nothing in the window moves on its own while nobody looks at it:
		// the pointer is elsewhere, and the levels, the record and the smoke
		// would only ask for frames to show what nobody is looking at. The
		// frame that shows the window again draws whole.
		//
		// Another app having the keyboard is not the same as nobody
		// looking, though: the window may stand in front of that app, as one
		// playing music over a browser does, and its lyrics must go on then.
		// So shown is kept as the window tells it, and the two together
		// decide.
		mygo.App.OnDidBecomeActive(func() {
			old := a.focused
			a.focused = true
			if a.win != nil {
				a.win.Invalidate()
			}
			// The glide stopped when the app left the screen, and the
			// lyrics have moved on since: it settles where they are now
			// rather than sliding in as the window shows again.
			if !old && a.npOpen {
				a.lyricAwake = true
			}
		})
		mygo.App.OnDidResignActive(func() { a.focused = false })
		a.win.OnShow(func() { a.shown = true })
		a.win.OnHide(func() { a.shown = false })
		a.applyTray()
		if shotHook != nil {
			shotHook(a)
		}
		// Closing the window hides it: music plays on, and the Dock icon
		// or the tray's brings it back. A Linux desktop without a tray
		// has neither, so there closing it quits.
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			if runtime.GOOS == "darwin" || a.tray != nil {
				a.win.Hide()
			} else {
				mygo.App.Quit()
			}
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.settings.Volume = a.volume
		a.settings.save()
		a.saveSession()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
		// The sources' runtimes take a moment each to stop: they are
		// stopped as the app goes rather than waited for.
		go a.closeSources()
	})
	// Windows leaves an icon in the notification area after its app until
	// the pointer passes over it.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { a.showMain() })
	return mygo.App.Run()
}
