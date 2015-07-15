package plotbot

import (
	"time"

	"github.com/gorilla/mux"
	"github.com/nlopes/slack"
)

//
// Bot plugins
//

type Plugin interface{}

type ChatPlugin interface {
	// Handle handles incoming messages matching the constraints
	// from ChatConfig.
	InitChatPlugin(*Bot)
}

type ChatConfig struct {
	// Whether to handle the bot's own messages
	EchoMessages bool

	// Whether to handle messages that are not destined to me
	OnlyMentions bool
}

type WebServer interface {
	InitWebServer(*Bot, []string)
	ServeWebRequests()
	PrivateRouter() *mux.Router
	PublicRouter() *mux.Router
}

// WebPlugin initializes plugins with a `Bot` instance, a `privateRouter` and a `publicRouter`. All URLs handled by the `publicRouter` must start with `/public/`.
type WebPlugin interface {
	InitWebPlugin(*Bot, *mux.Router, *mux.Router)
}

type Rewarder interface {
	InitRewarder(*Bot)
	RegisterBadge(shortName, title, description string)
	LogEvent(user *slack.User, event string, data interface{}) error
	FetchEventsSince(user *slack.User, since time.Time, event string, data interface{}) error
	FetchLastEvent(user *slack.User, event string, data interface{}) error
	FetchLastNEvents(user *slack.User, num int, event string, data interface{}) error
	AwardBadge(bot *Bot, user *slack.User, shortName string) error
}

var registeredPlugins = make([]Plugin, 0)

func RegisterPlugin(plugin Plugin) {
	registeredPlugins = append(registeredPlugins, plugin)
}

func InitChatPlugins(bot *Bot) {
	for _, plugin := range registeredPlugins {
		chatPlugin, ok := plugin.(ChatPlugin)
		if ok {
			chatPlugin.InitChatPlugin(bot)
		}
	}
}

func InitWebServer(bot *Bot, enabledPlugins []string) {
	for _, plugin := range registeredPlugins {
		webServer, ok := plugin.(WebServer)
		if ok {
			webServer.InitWebServer(bot, enabledPlugins)
			bot.WebServer = webServer
			return
		}
	}
}

func InitWebPlugins(bot *Bot) {
	if bot.WebServer == nil {
		return
	}

	for _, plugin := range registeredPlugins {
		webPlugin, ok := plugin.(WebPlugin)
		if ok {
			webPlugin.InitWebPlugin(bot, bot.WebServer.PrivateRouter(), bot.WebServer.PublicRouter())
		}
	}
}

func InitRewarder(bot *Bot) {
	for _, plugin := range registeredPlugins {
		rewarder, ok := plugin.(Rewarder)
		if ok {
			rewarder.InitRewarder(bot)
			bot.Rewarder = rewarder
			return
		}
	}
}
