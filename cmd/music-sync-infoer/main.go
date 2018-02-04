// This package is the main package of the music-sync player
package main

import (
	"fmt"
	"github.com/LogicalOverflow/music-sync/cmd"
	"github.com/LogicalOverflow/music-sync/comm"
	"github.com/LogicalOverflow/music-sync/logging"
	"github.com/LogicalOverflow/music-sync/schedule"
	"github.com/gdamore/tcell"
	"github.com/urfave/cli"
	"os"
	"time"
)

const usage = "run a music-sync client in info mode, which connects to a server and prints information about the current song"

func main() {
	app := cmd.NewApp(usage)
	app.Action = run
	app.Flags = []cli.Flag{
		cmd.ServerAddressFlag,
		cmd.ServerPortFlag,

		cmd.SampleRateFlag,
		cmd.LyricsHistorySizeFlag,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var lyricsHistorySize int

func run(ctx *cli.Context) error {
	// disable logging
	log.DefaultCutoffLevel = log.LevelOff
	var (
		serverAddress = ctx.String(cmd.FlagKey(cmd.ServerAddressFlag))
		serverPort    = ctx.Int(cmd.FlagKey(cmd.ServerPortFlag))

		sampleRate = ctx.Int(cmd.FlagKey(cmd.SampleRateFlag))
	)
	lyricsHistorySize = int(ctx.Uint(cmd.FlagKey(cmd.LyricsHistorySizeFlag)))

	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	s, e := tcell.NewScreen()
	if e != nil {
		fmt.Fprintf(os.Stderr, "%v\n", e)
		os.Exit(1)
	}
	if e = s.Init(); e != nil {
		fmt.Fprintf(os.Stderr, "%v\n", e)
		os.Exit(1)
	}

	s.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
	s.Clear()

	schedule.SampleRate = sampleRate

	server := fmt.Sprintf("%s:%d", serverAddress, serverPort)
	sender, err := comm.ConnectToServer(server, newInfoerPackageHandler())
	if err != nil {
		cli.NewExitError(err, 1)
	}

	go schedule.Infoer(sender)

	tcellLoop(s)

	return nil
}

func fmtDuration(duration time.Duration) string {
	if duration < time.Hour {
		return fmt.Sprintf("%d:%02d", duration/time.Minute, duration/time.Second%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", duration/time.Hour, duration/time.Minute%60, duration/time.Second%60)
}

func tcellLoop(screen tcell.Screen) {
	d := &drawer{Screen: screen}
	d.w, d.h = d.Size()

	running := true
	go func() {
		d.eventLoop()
		running = false
	}()

	for range time.Tick(200 * time.Millisecond) {
		if !running {
			break
		}
		redraw(d)
	}

	screen.Fini()
}

func redraw(d *drawer) {
	d.Clear()

	info := currentState.Info()
	currentSong := info.CurrentSong
	currentSample := info.CurrentSample

	songLength := time.Duration(0)
	timeInSong := time.Duration(0)
	progressInSong := float64(0)
	if currentSong.startIndex != 0 && int64(currentSong.startIndex) < currentSample {
		sampleInSong := currentSample - int64(currentSong.startIndex) - info.PausesInCurrentSong
		timeInSong = time.Duration(sampleInSong) * time.Second / time.Duration(schedule.SampleRate) / time.Nanosecond
		if 0 < currentSong.length {
			progressInSong = float64(sampleInSong) / float64(currentSong.length)
		}
		songLength = time.Duration(currentSong.length) * time.Second / time.Duration(schedule.SampleRate) / time.Nanosecond
	}

	songLineName := ""
	if currentSong.metadata.Title != "" {
		songLineName = currentSong.metadata.Title
	} else {
		songLineName = currentSong.filename
	}

	songLineArtistAlbum := ""
	if currentSong.metadata.Artist != "" {
		songLineArtistAlbum = currentSong.metadata.Artist
		if currentSong.metadata.Album != "" {
			songLineArtistAlbum += " - " + currentSong.metadata.Album
		}
	}

	timeLine := fmt.Sprintf("%s/%s", fmtDuration(timeInSong), fmtDuration(songLength))

	volumeLine := fmt.Sprintf("Volume: %06.2f%%", info.Volume*100)

	d.drawString(d.w-len(volumeLine)-1, d.h-4, tcell.StyleDefault, volumeLine)
	d.drawString(1, d.h-4, tcell.StyleDefault, songLineName)
	d.drawString(d.w-len(timeLine)-1, d.h-3, tcell.StyleDefault, timeLine)
	d.drawString(1, d.h-3, tcell.StyleDefault, songLineArtistAlbum)

	d.drawProgress(1, d.h-2, tcell.StyleDefault, d.w-2, progressInSong)

	d.drawBox(0, d.h-5, d.w, 5, tcell.StyleDefault)
	d.drawString(2, d.h-5, tcell.StyleDefault, info.playingString())

	lyricsHeight := lyricsHistorySize
	if d.h < lyricsHeight+7 {
		lyricsHeight = d.h - 7
	}
	if 0 < lyricsHeight {
		lines := lyricsHistory(lyricsHeight, currentSong, timeInSong)
		for i, l := range lines {
			d.drawString(1, d.h-7-i, tcell.StyleDefault, l)
		}
		d.drawBox(0, d.h-7-lyricsHeight, d.w, lyricsHeight+2, tcell.StyleDefault)
		d.drawString(2, d.h-7-lyricsHeight, tcell.StyleDefault, "Lyrics")
	}

	d.Show()
}

func lyricsHistory(height int, song upcomingSong, timeInSong time.Duration) []string {
	if song.lyrics != nil && 0 < len(song.lyrics) {
		nextLine := 0
		for ; nextLine < len(song.lyrics); nextLine++ {
			l := song.lyrics[nextLine]
			if l != nil && 0 < len(l) && int64(timeInSong/time.Millisecond) < l[0].Timestamp {
				break
			}
		}

		lines := make([]string, height)

		for i := range lines {
			lines[i] = ""
			if 0 <= nextLine-i-1 {
				for _, atom := range song.lyrics[nextLine-i-1] {
					if atom.Timestamp < int64(timeInSong/time.Millisecond)+100 {
						lines[i] += atom.Caption
					}
				}
			}
		}

		return lines
	}
	return make([]string, height)
}
