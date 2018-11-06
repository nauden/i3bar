// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// sample-bar demonstrates a sample i3bar built using barista.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"barista.run"
	"barista.run/bar"
	"barista.run/base/click"
	"barista.run/base/watchers/netlink"
	"barista.run/colors"
	"barista.run/modules/battery"
	"barista.run/modules/clock"
	"barista.run/modules/cputemp"
	"barista.run/modules/media"
	"barista.run/modules/meminfo"
	"barista.run/modules/netspeed"
	"barista.run/modules/sysinfo"
	"barista.run/modules/volume"
	"barista.run/modules/weather"
	"barista.run/modules/weather/openweathermap"
	"barista.run/modules/wlan"
	"barista.run/outputs"
	"barista.run/pango"
	"barista.run/pango/icons/fontawesome"
	"barista.run/pango/icons/material"
	"barista.run/pango/icons/mdi"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/martinlindhe/unit"
)

var spacer = pango.Text(" ").XXSmall()

func truncate(in string, l int) string {
	if len([]rune(in)) <= l {
		return in
	}
	return string([]rune(in)[:l-1]) + "⋯"
}

func hms(d time.Duration) (h int, m int, s int) {
	h = int(d.Hours())
	m = int(d.Minutes()) % 60
	s = int(d.Seconds()) % 60
	return
}

func formatMediaTime(d time.Duration) string {
	h, m, s := hms(d)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func mediaFormatFunc(m media.Info) bar.Output {
	if m.PlaybackStatus == media.Stopped || m.PlaybackStatus == media.Disconnected {
		return nil
	}
	artist := truncate(m.Artist, 20)
	title := truncate(m.Title, 40-len(artist))
	if len(title) < 20 {
		artist = truncate(m.Artist, 40-len(title))
	}
	iconAndPosition := pango.Icon("fa-music").Color(colors.Hex("#f70"))
	if m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(
			spacer, pango.Textf("%s/%s",
				formatMediaTime(m.Position()),
				formatMediaTime(m.Length)),
		)
	}
	return outputs.Pango(iconAndPosition, spacer, title, " - ", artist)
}

var startTaskManager = click.RunLeft("xfce4-taskmanager")

func home(path string) string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return filepath.Join(usr.HomeDir, path)
}

type freegeoipResponse struct {
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

func whereami() (lat float64, lng float64, err error) {
	resp, err := http.Get("https://freegeoip.app/json/")
	if err != nil {
		return 0, 0, err
	}
	var res freegeoipResponse
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return 0, 0, err
	}
	return res.Lat, res.Lng, nil
}

type autoWeatherProvider struct{}

func (a autoWeatherProvider) GetWeather() (weather.Weather, error) {
	lat, lng, err := whereami()
	if err != nil {
		return weather.Weather{}, err
	}
	return openweathermap.
		New("%%OWM_API_KEY%%").
		Coords(lat, lng).
		GetWeather()
}

func nag(msg string) {
	exec.Command("swaynag", "-m", msg).Start()
}

// LoadBarConfig automatically loads colors from the current bar's
// configuration. It assumes that the parent process is the i3bar instance,
// and the bar_id command-line flag identifies the bar id.
func LoadBarConfig() error {
	i3barPid := os.Getppid()
	i3barCmdline, err := ioutil.ReadFile(
		fmt.Sprintf("/proc/%d/cmdline", i3barPid))
	if err != nil {
		return err
	}
	barID := "bar-0"
	for _, arg := range bytes.Split(i3barCmdline, []byte{0}) {
		arg := string(arg)
		if strings.HasPrefix(arg, "--bar_id=") {
			barID = strings.TrimPrefix(arg, "--bar_id=")
			break
		}
	}

	var parsed struct {
		Colors map[string]string `json:"colors"`
	}

	out, err := exec.Command("i3-msg", "-t", "get_bar_config", barID).Output()
	if err != nil {
		return err
	}

	if err = json.Unmarshal(out, &parsed); err != nil {
		return err
	}

	colors.LoadFromMap(parsed.Colors)
	return nil
}

func main() {
	/*
		typicons.Load(home("Github/typicons.font"))
		ionicons.LoadMd(home("Github/ionicons")) */
	fontawesome.Load(home(".icons/FontAwesome"))
	material.Load(home(".icons/Material"))
	mdi.Load(home(".icons/MaterialDesign-Webfont"))

	err := LoadBarConfig()
	if err != nil {
		nag(err.Error())
	}

	bg := colors.Scheme("background")
	fg := colors.Scheme("statusline")
	if fg != nil && bg != nil {
		iconColor := fg.Colorful().BlendHcl(bg.Colorful(), 0.5).Clamped()
		colors.Set("dim-icon", iconColor)
		_, _, v := fg.Colorful().Hsv()
		if v < 0.3 {
			v = 0.3
		}
		colors.Set("bad", colorful.Hcl(40, 1.0, v).Clamped())
		colors.Set("degraded", colorful.Hcl(90, 1.0, v).Clamped())
		colors.Set("good", colorful.Hcl(120, 1.0, v).Clamped())
	}

	localtime := clock.Local().
		Output(time.Second, func(now time.Time) bar.Output {
			return outputs.Pango(
				pango.Icon("fa-calendar").Color(colors.Scheme("dim-icon")), spacer,
				now.Format("Mon Jan 2"), spacer,
				pango.Icon("fa-clock").Color(colors.Scheme("dim-icon")), spacer,
				now.Format("15:04:05"),
			).OnClick(click.RunLeft("gsimplecal"))
		})

	buildBattOutput := func(i battery.Info, disp *pango.Node) *bar.Segment {
		if i.Status == battery.Disconnected || i.Status == battery.Unknown {
			return nil
		}
		iconName := "battery"
		if i.Status == battery.Charging {
			iconName += "-charging"
		}
		tenth := i.RemainingPct() / 10
		switch {
		case tenth == 0:
			iconName += "-outline"
		case tenth < 10:
			iconName += fmt.Sprintf("-%d0", tenth)
		}
		out := outputs.Pango(pango.Icon("mdi-"+iconName), disp)
		switch {
		case i.RemainingPct() <= 5:
			out.Urgent(true)
		case i.RemainingPct() <= 15:
			out.Color(colors.Scheme("bad"))
		case i.RemainingPct() <= 25:
			out.Color(colors.Scheme("degraded"))
		}
		return out
	}
	var showBattPct, showBattTime func(battery.Info) bar.Output

	batt := battery.All()
	showBattPct = func(i battery.Info) bar.Output {
		out := buildBattOutput(i, pango.Textf("%d%%", i.RemainingPct()))
		if out == nil {
			return nil
		}
		return out.OnClick(click.Left(func() {
			batt.Output(showBattTime)
		}))
	}
	showBattTime = func(i battery.Info) bar.Output {
		rem := i.RemainingTime()
		out := buildBattOutput(i, pango.Textf(
			"%d:%02d", int(rem.Hours()), int(rem.Minutes())%60))
		if out == nil {
			return nil
		}
		return out.OnClick(click.Left(func() {
			batt.Output(showBattPct)
		}))
	}
	batt.Output(showBattPct)

	vol := volume.DefaultMixer().Output(func(v volume.Volume) bar.Output {
		if v.Mute {
			return outputs.
				Pango(pango.Icon("fa-volume-mute"), "MUT").
				Color(colors.Scheme("degraded"))
		}
		iconName := "off"
		pct := v.Pct()
		if pct > 66 {
			iconName = "up"
		} else if pct > 33 {
			iconName = "down"
		}
		return outputs.Pango(
			pango.Icon("fa-volume-"+iconName),
			spacer,
			pango.Textf("%2d%%", pct),
		)
	})

	loadAvg := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		out := outputs.Pango(pango.Icon("fa-hdd"), spacer, pango.Textf("%0.2f %0.2f", s.Loads[0], s.Loads[2]))
		// Load averages are unusually high for a few minutes after boot.
		if s.Uptime < 10*time.Minute {
			// so don't add colours until 10 minutes after system start.
			return out
		}
		switch {
		case s.Loads[0] > 128, s.Loads[2] > 64:
			out.Urgent(true)
		case s.Loads[0] > 64, s.Loads[2] > 32:
			out.Color(colors.Scheme("bad"))
		case s.Loads[0] > 32, s.Loads[2] > 16:
			out.Color(colors.Scheme("degraded"))
		}
		out.OnClick(startTaskManager)
		return out
	})

	freeMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		out := outputs.Pango(pango.Icon("fa-memory"), spacer, outputs.IBytesize(m.Available()))
		freeGigs := m.Available().Gigabytes()

		switch {
		case freeGigs < 0.5:
			out.Urgent(true)
		case freeGigs < 1:
			out.Color(colors.Scheme("bad"))
		case freeGigs < 2:
			out.Color(colors.Scheme("degraded"))
		case freeGigs > 3:
			out.Color(colors.Scheme("good"))
		}
		out.OnClick(startTaskManager)
		return out
	})

	temp := cputemp.New().
		RefreshInterval(2 * time.Second).
		Output(func(temp unit.Temperature) bar.Output {
			out := outputs.Pango(
				pango.Icon("mdi-fan"), spacer,
				pango.Textf("%2d℃", int(temp.Celsius())),
			)
			switch {
			case temp.Celsius() > 60:
				out.Urgent(true)
			case temp.Celsius() > 50:
				out.Color(colors.Scheme("bad"))
			case temp.Celsius() > 40:
				out.Color(colors.Scheme("degraded"))
			}
			return out
		})

	sub := netlink.Any()
	iface := (<-sub).Name
	sub.Unsubscribe()
	net := netspeed.New(iface).
		RefreshInterval(2 * time.Second).
		Output(func(s netspeed.Speeds) bar.Output {
			return outputs.Pango(
				pango.Icon("fa-upload"), spacer, pango.Textf("%7s", outputs.Byterate(s.Tx)),
				pango.Text(" ").Small(),
				pango.Icon("fa-download"), spacer, pango.Textf("%7s", outputs.Byterate(s.Rx)),
			)
		})

	wl := wlan.Any().Output(func(i wlan.Info) bar.Output {
		wicon := pango.Icon("fa-signal")
		switch {
		case !i.Enabled():
			return nil
		case i.Connecting():
			return pango.Text("W: ...").Color(colors.Scheme("bad"))
		case !i.Connected():
			return pango.Text("W: down").Color(colors.Scheme("degraded"))
		case len(i.IPs) < 1:
			return pango.Textf("%s (...)", i.SSID)
		default:
			return outputs.Pango(wicon, spacer, pango.Textf("%s (%s)", i.SSID, i.IPs[0]))
		}
	})

	panic(barista.Run(
		wl,
		net,
		temp,
		freeMem,
		loadAvg,
		vol,
		batt,
		localtime,
	))
}
