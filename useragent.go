package deeplink

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Platform and device-class values used in [ClickEvent] and analytics
// breakdowns.
const (
	platformWeb     = "web"
	platformAndroid = "android"
	platformIOS     = "ios"

	deviceMobile  = "mobile"
	deviceDesktop = "desktop"
	deviceBot     = "bot"
)

// newClickEvent builds a [ClickEvent] from an incoming resolve request.
// All parsing is heuristic and best-effort; unknown fields are left empty.
func newClickEvent(r *http.Request, shortID string) ClickEvent {
	platform, device, os, browser := parseUserAgent(r.UserAgent())
	return ClickEvent{
		ShortID:  shortID,
		Time:     time.Now().UTC(),
		Platform: platform,
		Device:   device,
		OS:       os,
		Browser:  browser,
		Referrer: referrerHost(r.Referer()),
	}
}

// parseUserAgent derives a coarse platform, device class, OS, and browser
// from a UA string — enough for breakdowns, not a full UA database.
func parseUserAgent(ua string) (platform, device, os, browser string) {
	if ua == "" {
		return platformWeb, deviceDesktop, "", ""
	}
	l := strings.ToLower(ua)

	switch {
	case isBot(l):
		return platformWeb, deviceBot, "", ""
	case strings.Contains(l, "android"):
		platform, device, os = platformAndroid, deviceMobile, "Android"
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipod"):
		platform, device, os = platformIOS, deviceMobile, "iOS"
	case strings.Contains(l, "ipad"):
		platform, device, os = platformIOS, deviceMobile, "iPadOS"
	default:
		platform, device = platformWeb, deviceDesktop
		os = desktopOS(l)
	}

	return platform, device, os, browserFamily(l)
}

func isBot(l string) bool {
	for _, sig := range []string{"bot", "crawler", "spider", "facebookexternalhit", "embedly", "slackbot", "telegrambot", "whatsapp", "twitterbot", "discordbot"} {
		if strings.Contains(l, sig) {
			return true
		}
	}
	return false
}

func desktopOS(l string) string {
	switch {
	case strings.Contains(l, "windows"):
		return "Windows"
	case strings.Contains(l, "mac os") || strings.Contains(l, "macintosh"):
		return "macOS"
	case strings.Contains(l, "linux"):
		return "Linux"
	default:
		return ""
	}
}

// browserFamily checks Edge/Opera/Firefox before Chrome before Safari, since
// their UA strings are supersets of each other. Mobile brand tokens (EdgA,
// FxiOS, CriOS, …) are matched so iOS/Android variants aren't all collapsed.
func browserFamily(l string) string {
	switch {
	case strings.Contains(l, "edg/") || strings.Contains(l, "edga/") || strings.Contains(l, "edgios/") || strings.Contains(l, "edge/"):
		return "Edge"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opt/") || strings.Contains(l, "opera"):
		return "Opera"
	case strings.Contains(l, "firefox") || strings.Contains(l, "fxios"):
		return "Firefox"
	case strings.Contains(l, "crios") || strings.Contains(l, "chrome"):
		return "Chrome"
	case strings.Contains(l, "safari"):
		return "Safari"
	default:
		return ""
	}
}

// referrerHost returns the host of a Referer header value, or "" when the
// header is absent or unparseable.
func referrerHost(referer string) string {
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return u.Host
}
