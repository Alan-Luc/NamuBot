package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	oppai "github.com/flesnuk/oppai5"
	"gopkg.in/irc.v3"
)

const (
	ApiBaseUrl = "https://osu.ppy.sh/api/"
)

var (
	Token  string
	Config ConfigData
)

type ConfigData struct {
	BanchoUser      string
	BanchoPass      string
	DiscordBotToken string
	OsuApiKey       string
}

type ApiData []struct {
	BeatmapID        string `json:"beatmap_id"`
	HitLength        string `json:"hit_length"`
	Version          string `json:"version"`
	Artist           string `json:"artist"`
	Title            string `json:"title"`
	Difficultyrating string `json:"difficultyrating"`
}

func init() {
	readin, err := ioutil.ReadFile("./config.json")
	if err != nil {
		log.Fatalln("Error: " + err.Error())
	}
	_ = json.Unmarshal(readin, &Config)
}

func httpClient() *http.Client {
	client := &http.Client{Timeout: 10 * time.Second}
	return client
}

func httpRequest(client *http.Client, method string, url string) ([]byte, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	res, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return body, nil
}

func sendSongReq(c *irc.Client, out <-chan string) {
	for {
		msg := <-out
		c.WriteMessage(&irc.Message{
			Command: "PRIVMSG",
			Params: []string{
				// Config.BanchoUser,
				"ananabnosna",
				msg,
			},
		})
	}

}

func bancho(out <-chan string) {
	for {
		conn, err := net.Dial("tcp", "cho.ppy.sh:6667")
		if err != nil {
			log.Println("Bancho failed to connect, attempting to reconnect (5s)")
		} else {
			config := irc.ClientConfig{
				Nick: Config.BanchoUser,
				Pass: Config.BanchoPass,

				Handler: irc.HandlerFunc(func(c *irc.Client, m *irc.Message) {
					banchoHandler(c, m, out)
				}),
			}

			client := irc.NewClient(conn, config)
			err = client.Run()
			if err != nil {
				log.Println("Bancho Disconnected, attempting to reconnect (5s)")
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func banchoHandler(c *irc.Client, m *irc.Message, out <-chan string) {
	if m.Command == "001" {
		// c.Write("JOIN " + Config.BanchoUser)
		c.Write("JOIN ananabnosna")
		log.Println("Connected to Bancho")
		go sendSongReq(c, out)
	} else if m.Command == "PING" {
		c.Write("PONG")
	}
}

func discord(out chan<- string) {
	disc, err := discordgo.New("Bot " + Config.DiscordBotToken)
	if err != nil {
		log.Fatal("Failed to create a Discord session,", err)
	} else {

		disc.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			discordHandler(s, m, out)
		})
		disc.Identify.Intents = discordgo.IntentsGuildMessages

		err = disc.Open()
		if err != nil {
			log.Fatal("Failed to connect to Discord,", err)
		}
	}
}

func discordErrMsg(channel, errMsg string, s *discordgo.Session) {
	_, err := s.ChannelMessageSend(channel, errMsg)
	if err != nil {
		log.Println("Something went wrong sending the error message,", err)
		return
	}
}

func modCheck(msg, mString string, mods uint32) (string, uint32) {
	// ty monko
	hd := regexp.MustCompile(`(?i)(hd)|(hidden)`)
	hr := regexp.MustCompile(`(?i)(hr)|(hardrock)|(hard rock)`)
	dt := regexp.MustCompile(`(?i)(dt)|(nc)|(doubletime)|(double time)|(nightcore)|(night core)`)
	ez := regexp.MustCompile(`(?i)(ez)|(easy)`)
	fl := regexp.MustCompile(`(?i)(fl)|(flashlight)|(flash light)`)
	ht := regexp.MustCompile(`(?i)(ht[^t])|(ht$)|(halftime)|(half time)`)

	if hd.MatchString(msg) {
		mString += "HD,"
		mods += (1 << 3)
	}
	if hr.MatchString(msg) {
		mString += "HR,"
		mods += (1 << 4)
	}
	if dt.MatchString(msg) {
		mString += "DT,"
		mods += (1 << 6)
	}
	if ez.MatchString(msg) {
		mString += "EZ,"
		mods += (1 << 1)
	}
	if fl.MatchString(msg) {
		mString += "FL,"
	}
	if ht.MatchString(msg) {
		mString += "HT,"
		mods += (1 << 8)
	}
	if strings.HasSuffix(mString, ",") {
		mString = strings.TrimSuffix(mString, ",")
		mString += " "
	}

	return mString, mods
}

func discordHandler(s *discordgo.Session, m *discordgo.MessageCreate, out chan<- string) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	message := m.Content
	errMsg := fmt.Sprintf("<@%s> you may have requested with an invalid link!", m.Author.ID)

	// ty monko
	urlRegex := regexp.MustCompile(`https:\S+`)
	if urlRegex.MatchString(message) {
		link := urlRegex.FindString(message)
		var isValidLink bool

		// ty monko
		underterminedLinkPat := regexp.MustCompile(`^https:\/\/osu.ppy.sh\/beatmapsets`)
		if underterminedLinkPat.MatchString(message) {
			isValidLink = strings.Contains(link, "#osu")
		} else {
			// ty monko
			osuLinkRegex := regexp.MustCompile(`(^https:\/\/osu.ppy.sh\/b\/)|(^https:\/\/old.ppy.sh\/b\/)|(^https:\/\/osu.ppy.sh\/beatmaps)`)
			isValidLink = osuLinkRegex.MatchString(link)
		}
		if isValidLink {
			// beatmapIdRegex := regexp.MustCompile(`([^/]+)/?$`)

			// find all strings of numbers in url sections
			beatmapIdRegex := regexp.MustCompile(`(?P<number>\d+)`)

			if beatmapIdRegex.MatchString(link) {
				matches := beatmapIdRegex.FindAllString(link, -1)
				beatmapId := matches[len(matches)-1]

				url := ApiBaseUrl + "get_beatmaps?k=" + Config.OsuApiKey + "&b=" + beatmapId
				c := httpClient()
				res, err := httpRequest(c, "GET", url)
				if err != nil {
					log.Println("something went wrong with fetch,", err)
					discordErrMsg(m.ChannelID, errMsg, s)
					return
				}

				var data ApiData

				if err := json.Unmarshal(res, &data); err != nil {
					log.Println(err.Error())
					return
				}
				if len(data) < 1 {
					log.Println("something went wrong with fetch")
					discordErrMsg(m.ChannelID, errMsg, s)
					return
				}
				song := data[0]

				var params oppai.Parameters
				var mods uint32
				var modstring string
				dt := regexp.MustCompile(`(?i)(dt)|(nc)|(doubletime)|(double time)|(nightcore)|(night core)`)
				ht := regexp.MustCompile(`(?i)(ht[^t])|(ht$)|(halftime)|(half time)`)

				modstring, mods = modCheck(message, "", 0)

				if mods > 0 {
					modstring = "+ " + modstring
					params.Mods = mods
				}

				url = "https://osu.ppy.sh/osu/" + song.BeatmapID
				res, err = httpRequest(c, "GET", url)
				if err != nil {
					log.Println("something went wrong with fetch,", err)
					discordErrMsg(m.ChannelID, errMsg, s)
					return
				}

				beatmap := oppai.Parse(bytes.NewReader(res))
				ppInfo := oppai.PPInfo(beatmap, &params)
				starRating, _ := strconv.ParseFloat(song.Difficultyrating, 64)
				pp := ppInfo.StepPP

				if mods > 0 {
					starRating = ppInfo.Diff.Total
				}

				hitLength, _ := strconv.Atoi(song.HitLength)
				if dt.MatchString(message) {
					hitLength = hitLength * 2 / 3
				} else if ht.MatchString(message) {
					hitLength = hitLength * 3 / 2
				}

				formattedLength := fmt.Sprintf("%d:%02d", hitLength/60, hitLength%60)
				formattedSR := fmt.Sprintf("%.2f", starRating)
				formattedPP := fmt.Sprintf("98%%: %.2fpp | 99%%: %.2fpp | 100%%: %.2fpp", pp.P98, pp.P99, pp.P100)

				songInfo := fmt.Sprintf("%s - %s [%s]", song.Artist, song.Title, song.Version)
				beatmapLink := fmt.Sprintf("[https://osu.ppy.sh/b/%s %s] %s|", song.BeatmapID, songInfo, modstring)
				beatmapInfo := fmt.Sprintf("length: %v | sr: %v* | (%v)", formattedLength, formattedSR, formattedPP)
				mapMessage := fmt.Sprintf("%s >>> %s %s", m.Author.Username, beatmapLink, beatmapInfo)
				discMessage := fmt.Sprintf("<@%s>  >>> %s %s\n%s", m.Author.ID, songInfo, modstring, beatmapInfo)

				log.Println(mapMessage)
				out <- mapMessage

				_, err = s.ChannelMessageSend(m.ChannelID, discMessage)
				if err != nil {
					log.Println(err)
					return
				}
			}
		} else {
			log.Printf("%s requested with an invalid link", m.Author.Username)
			return
		}
	}
}

func main() {
	requests := make(chan string)
	fmt.Println("NamuBot is running... Press ctrl+c to exit.")

	go discord(requests)
	go bancho(requests)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
}
