package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/GeertJohan/go.rice"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/plotly/plotbot"
	"golang.org/x/oauth2"
)

var web *Webapp

type Webapp struct {
	config         *WebappConfig
	store          *sessions.CookieStore
	bot            *plotbot.Bot
	handler        *negroni.Negroni
	privateRouter  *mux.Router
	publicRouter   *mux.Router
	enabledPlugins []string
}

type WebappConfig struct {
	Listen            string `json:"listen"`
	RedirectURL       string `json:"oauth_base_url"`
	ClientID          string `json:"client_id"`
	ClientSecret      string `json:"client_secret"`
	RestrictDomain    string `json:"restrict_domain"`
	SessionAuthKey    string `json:"session_auth_key"`
	SessionEncryptKey string `json:"session_encrypt_key"`
}

func init() {
	plotbot.RegisterPlugin(&Webapp{})
}

func (webapp *Webapp) InitWebServer(bot *plotbot.Bot, enabledPlugins []string) {
	var conf struct {
		Webapp WebappConfig
	}
	bot.LoadConfig(&conf)

	webapp.bot = bot
	webapp.enabledPlugins = enabledPlugins
	webapp.config = &conf.Webapp
	webapp.store = sessions.NewCookieStore([]byte(conf.Webapp.SessionAuthKey), []byte(conf.Webapp.SessionEncryptKey))
	webapp.privateRouter = mux.NewRouter()
	webapp.publicRouter = mux.NewRouter()

	configureWebapp(&conf.Webapp)

	webapp.privateRouter.HandleFunc("/", webapp.handleRoot)
	web = webapp
}

func (webapp *Webapp) PrivateRouter() *mux.Router {
	return webapp.privateRouter
}
func (webapp *Webapp) PublicRouter() *mux.Router {
	return webapp.publicRouter
}

func (webapp *Webapp) ServeWebRequests() {
	privMux := http.NewServeMux()
	privMux.Handle("/static/", http.StripPrefix("/static", http.FileServer(rice.MustFindBox("static").HTTPBox())))
	privMux.Handle("/", webapp.privateRouter)

	pubMux := http.NewServeMux()
	pubMux.Handle("/public/", webapp.publicRouter)
	pubMux.Handle("/", NewOAuthMiddleware(privMux))

	webapp.handler = negroni.Classic()
	webapp.handler.UseHandler(context.ClearHandler(pubMux))

	webapp.handler.Run(webapp.config.Listen)
}

// func LaunchWebapp(b *plotbot.Bot) {

// 	rt.HandleFunc("/send_notif", handleNotif)
// 	rt.HandleFunc("/hipchat/users", handleGetUsers)

// 	n.Run("localhost:8080")
// }

func configureWebapp(conf *WebappConfig) {
	oauthCfg = &oauth2.Config{
		ClientID:     conf.ClientID,
		ClientSecret: conf.ClientSecret,
		RedirectURL:  conf.RedirectURL + "/oauth2callback",
		Scopes:       []string{"openid", "profile", "email", "https://www.googleapis.com/auth/userinfo.profile"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://accounts.google.com/o/oauth2/token",
		},
	}
}

func (webapp *Webapp) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	profile, _ := checkAuth(r)

	tpl, err := getRootTemplate()
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	ctx := struct {
		CurrentUser, EnabledPlugins template.JS
	}{
		profile.AsJavascript(),
		webapp.getEnabledPluginsJS(),
	}
	tpl.Execute(w, ctx)
}

func getRootTemplate() (*template.Template, error) {
	box, err := rice.FindBox("static")
	if err != nil {
		return nil, fmt.Errorf("Error finding static assets: %s", err)
	}

	rawTpl, err := box.String("index.html")
	if err != nil {
		return nil, fmt.Errorf("Error loading index.html: %s", err)
	}

	tpl, err := template.New("index.html").Parse(rawTpl)
	if err != nil {
		return nil, fmt.Errorf("Cannot parse index.html: %s", err)
	}

	return tpl, nil
}

func (webapp *Webapp) getEnabledPluginsJS() template.JS {
	out := make(map[string]bool)
	for _, pluginName := range webapp.enabledPlugins {
		out[pluginName] = true
	}

	jsonMap, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatal("Couldn't marshal EnabledPlugins list for rendering", err)
		return template.JS("{}")
	}
	return template.JS(jsonMap)
}
