package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"github.com/esimov/pigo/core"
	"github.com/nlopes/slack"
	"github.com/pkg/errors"
	"regexp"
	"unicode"
)

type Player struct {
	round, correct, incorrect, rightAnswer int
	state                                  UserState
	listOfAnswers                          []int
}

type Actions int
type UserState int

const (
	PLAY    Actions = 1
	GIVEUP  Actions = 2
	ENDGAME Actions = 3
	//NOTIFY  Actions = 4
	OPTION Actions = 5
	NOSTATE           UserState = 6
	WAITINGFORCOMMAND UserState = 7
	WAITINGFORGUESS   UserState = 8
)

func (user *Player) IncrementCorrect() {
	user.correct++
}

func (user *Player) IncrementIncorrect() {
	user.incorrect++
}

func (user *Player) IncrementRound() {
	user.round++
}

func (user *Player) SetCorrect(correct int) {
	user.correct = correct
}

func (user *Player) SetIncorrect(incorrect int) {
	user.incorrect = incorrect
}

func (user *Player) SetRightAnswer(rightAnswer int) {
	user.rightAnswer = rightAnswer
}

func (user *Player) SetRound(round int) {
	user.round = round
}

func (user *Player) SetState(state UserState) {
	user.state = state
}

var (
	api         = slack.New(os.Getenv("SLACK_TOKEN"))
	directory   []slack.User
	players     = map[string]*Player{}
	whichAction = map[string]Actions{
		"begin": PLAY,
		"go":    PLAY,
		"hello": PLAY,
		"hi":	 PLAY,
		"now":   PLAY,
		"play":  PLAY,
		"start": PLAY,
		"yes":   PLAY,
		"1":     PLAY,

		"continue":     GIVEUP,
		"dunno":        GIVEUP,
		"give up":      GIVEUP,
		"idk":          GIVEUP,
		"i dont know":  GIVEUP,
		"i don't know": GIVEUP,
		"i give up":    GIVEUP,
		"service":      GIVEUP,
		"support":      GIVEUP,
		"ugh":          GIVEUP,

		"bye":      ENDGAME,
		"end":      ENDGAME,
		"end game": ENDGAME,
		"finish":   ENDGAME,
		"no":       ENDGAME,
		"stop":     ENDGAME,
		"0":        ENDGAME,

		//"alert":         NOTIFY,
		//"alerts":        NOTIFY,
		//"notification":  NOTIFY,
		//"notifications": NOTIFY,
		//"notify":        NOTIFY,
		//"notify me":     NOTIFY,

		"advice":   OPTION,
		"aid":      OPTION,
		"assist":   OPTION,
		"command":  OPTION,
		"commands": OPTION,
		"control":  OPTION,
		"controls": OPTION,
		"help":     OPTION,
		"help me":  OPTION,
		"list":     OPTION,
		"ls":       OPTION,
		"option":   OPTION,
		"options":  OPTION,
	}
	cascadeFile = "data/facefinder"
	minSize     = flag.Int("min", 20, "Minimum size of face")
	maxSize     = flag.Int("max", 1000, "Maximum size of face")
	shiftFactor = flag.Float64("shift", 0.1, "Shift detection window by percentage")
	scaleFactor = flag.Float64("scale", 1.1, "Scale detection window by percentage")
	//first  = &Player{0, 0, -1, WAITINGFORCOMMAND, rand.Perm(len(directory))}
	//second = &Player{0, 0, -1, WAITINGFORCOMMAND, rand.Perm(len(directory))}
	//third  = &Player{0, 0, -1, WAITINGFORCOMMAND, rand.Perm(len(directory))}
)

func main() {
	api.SetDebug(true)
	rtm := api.NewRTM()
	go rtm.ManageConnection()
	stillRunning := true
	filterUsers()
	rand.Seed(time.Now().UnixNano())
	for stillRunning {
		select {
		case msg := <-rtm.IncomingEvents:
			fmt.Print("Event Received: ")
			switch ev := msg.Data.(type) {
			case *slack.ConnectedEvent:
				fmt.Println("Connection Counter:", ev.ConnectionCount)
			case *slack.MessageEvent:
				fmt.Printf("Message: %v\n", ev)
				info := rtm.GetInfo()
				_, playerExists := players[ev.User]
				if !playerExists && ev.User != info.User.ID {
					players[ev.User] = &Player{0, 0, 0, -1, NOSTATE, rand.Perm(len(directory))}
				}
				respondToText(rtm, ev)
			case *slack.RTMError:
				fmt.Printf("Error: %s\n", ev.Error())
			case *slack.InvalidAuthEvent:
				fmt.Printf("Invalid Credentials")
				stillRunning = false
			default:
				fmt.Printf("Chugging Along")
			}
		}
	}
}

func respondToText(rtm *slack.RTM, msg *slack.MessageEvent) {
	var response string
	var params slack.PostMessageParameters
	var slice []slack.Attachment
	text := strings.ToLower(msg.Text)
	text = strings.TrimFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	user := players[msg.User]
	switch user.state {
	case NOSTATE:
		response = "hey there! i'm namegame and i'm here to help you learn the names of your coworkers. type and enter go to start."
		rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
		user.SetState(WAITINGFORCOMMAND)
	case WAITINGFORCOMMAND:
		switch whichAction[text] {
		case PLAY:
			userProfile, err := api.GetUserInfo(msg.User)
			if err != nil {
				return
			}
			goodFace, err := facePresent(userProfile)
			if err != nil {
				fmt.Println(err.Error())
			}
			if !goodFace && err == nil {
				var params slack.PostMessageParameters
				var slice []slack.Attachment
				slice = append(slice, slack.Attachment{
					Color:    "f80909",
					Title:    "MIGHT WANNA CHANGE THIS",
					ImageURL: userProfile.Profile.Image192,
				})
				params.Attachments = slice
				response = "woah there friend. do you think your co-workers will recognize this???"
				rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
				rtm.PostMessage(msg.Channel, "", params)
			}
			user.SetRightAnswer(user.listOfAnswers[0])
			if len(user.listOfAnswers) > 0 {
				continuousPlay(rtm, msg, user.rightAnswer)
			} else {
				user.listOfAnswers = rand.Perm(len(directory))
				response = "you made it through everyone! i'll regenerate the list... let me know if you want to play again."
				rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
				user.SetState(WAITINGFORCOMMAND)
			}
		case ENDGAME:
			slice = append(slice, slack.Attachment{
				Color: "f9dc1b",
				Fields: []slack.AttachmentField{
					{
						Title: "Right",
						Value: strconv.Itoa(user.correct),
						Short: true,
					},
					{
						Title: "Wrong",
						Value: strconv.Itoa(user.incorrect),
						Short: true,
					},
				},
			})
			params.Attachments = slice
			response = "here are your scores, see you around."
			rtm.PostMessage(msg.Channel, "score", params)
			rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
			//scoreBoard(rtm, msg, user)
			user.SetRound(0)
			user.SetCorrect(0)
			user.SetIncorrect(0)
			user.SetState(NOSTATE)
			//case NOTIFY:
			//	sendNotifications(rtm, msg)
		case OPTION:
			response = "PLAY: enter go,start, 1, etc... to begin the game" +
				"\nEND: enter stop, no, bye, etc... to end the game" +
				"\nGIVE UP: enter dunno, idk, give up, etc... when you want to see the right answer"
			rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
		default:
			if strings.Contains(text, "show") {
				directoryLookUp(rtm, msg)
			} else {
				response = "hey friend! enter 'go' to start playing."
				rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
			}
		}
	case WAITINGFORGUESS:
		switch whichAction[text] {
		case GIVEUP:
			sendBio(rtm, msg, directory[user.rightAnswer])
			if len(user.listOfAnswers) > 1 {
				user.listOfAnswers = user.listOfAnswers[1:]
				user.SetRightAnswer(user.listOfAnswers[0])
			} else {
				user.listOfAnswers = rand.Perm(len(directory))
				response := "you made it through everyone! i'll regenerate the list ... enter go to play again."
				rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
				user.SetState(WAITINGFORCOMMAND)
			}
			user.IncrementIncorrect()
			user.SetState(WAITINGFORCOMMAND)
			continuousPlay(rtm, msg, user.rightAnswer)
		case ENDGAME:
			slice = append(slice, slack.Attachment{
				Color: "f9dc1b",
				Fields: []slack.AttachmentField{
					{
						Title: "Right",
						Value: strconv.Itoa(user.correct),
						Short: true,
					},
					{
						Title: "Wrong",
						Value: strconv.Itoa(user.incorrect),
						Short: true,
					},
				},
			})
			params.Attachments = slice
			response = "here are your scores, see you around."
			rtm.PostMessage(msg.Channel, "score", params)
			rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
			//scoreBoard(rtm, msg, user)
			user.SetRound(0)
			user.SetCorrect(0)
			user.SetIncorrect(0)
			user.SetState(NOSTATE)
		case OPTION:
			response = "PLAY: enter 'go', 'start', '1', etc... to begin the game" +
				"\nEND: enter 'stop', 'no', 'bye', etc... to end the game" +
				"\nGIVE UP: enter 'dunno', 'idk', 'give up', etc... when you want to see the right answer"
			rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
		default:
			if containsName(text, msg) {
				if len(user.listOfAnswers) > 1 {
					user.listOfAnswers = user.listOfAnswers[1:]
					user.SetRightAnswer(user.listOfAnswers[0])
					response = "nice job!"
					rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
					continuousPlay(rtm, msg, user.rightAnswer)
				} else {
					user.listOfAnswers = rand.Perm(len(directory))
					response := "nice job! you made it through everyone! i'll regenerate the list ... enter go to play again."
					defer rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
					user.SetState(WAITINGFORCOMMAND)
				}
				user.IncrementCorrect()
			} else {
				response = "whoops, not quite. try again or give up."
				user.IncrementIncorrect()
				rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
			}
		}
	}
}

func containsDuplicates(check slack.User, dir []slack.User) bool {
	for _, user := range dir {
		if strings.EqualFold(check.RealName, user.RealName) {
			return true
		}
	}
	return false
}

func containsName(text string, msg *slack.MessageEvent) bool {
	user := players[msg.User]
	return strings.Contains(strings.ToLower(directory[user.rightAnswer].Profile.FirstName), text) ||
		strings.Contains(strings.ToLower(directory[user.rightAnswer].Profile.LastName), text) ||
		strings.Contains(strings.ToLower(directory[user.rightAnswer].Profile.DisplayName), text) ||
		strings.Contains(strings.ToLower(directory[user.rightAnswer].Profile.RealName), text)
}

func continuousPlay(rtm *slack.RTM, msg *slack.MessageEvent, rightAnswer int) {
	var params slack.PostMessageParameters
	var slice []slack.Attachment
	var response string
	display := directory[rightAnswer]
	user := players[msg.User]
	slice = append(slice, slack.Attachment{
		Color:    "4094d1",
		Title:    "guess who",
		ImageURL: display.Profile.Image192,
	})
	params.Attachments = slice
	rtm.PostMessage(msg.Channel, "", params)
	user.SetState(WAITINGFORGUESS)
	switch user.round {
	case 0:
		response = "*here's a hint: if you don't know the name, enter 'idk', 'dunno', 'give up', etc... to get the answer.*"
	case 3:
		response = "*looks like you're getting the hang of things! if you ever need help, enter 'options', 'help', 'commands', etc... for a list of commands.*"
	case 6:
		response = "*once you get tired of playing and want to see your score, enter 'end', 'stop', 'finish', etc... to end the game.*"
	case 9:
		response = "*if you know a name but not the face, once you've ended your game, enter 'show <name>' to utilize the directory.*"
	}
	defer rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
	defer user.IncrementRound()
}

func directoryLookUp(rtm *slack.RTM, msg *slack.MessageEvent) {
	text := strings.ToLower(msg.Text)
	text = strings.TrimFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	for _, user := range directory {
		firstName := regexp.MustCompile(strings.ToLower(user.Profile.RealName))
		lastName := regexp.MustCompile(strings.ToLower(user.Profile.LastName))
		displayName := regexp.MustCompile(strings.ToLower(user.Profile.DisplayName))
		fullName := regexp.MustCompile(strings.ToLower(user.Profile.RealName))
		if firstName.MatchString(text) ||
			lastName.MatchString(text) ||
			displayName.MatchString(text) ||
			fullName.MatchString(text) {
			sendBio(rtm, msg, user)
			return
		}
	}
	rtm.SendMessage(rtm.NewOutgoingMessage("sorry, i don't think that name belongs to someone that works here.", msg.Channel))
}

func facePresent(user *slack.User) (bool, error) {
	cascadeFile, err := Asset(cascadeFile)
	if err != nil {
		log.Fatalf("Error reading the cascade file: %v", err)
	}
	sourceFile, err := http.Get(user.Profile.Image192)
	if err != nil {
		return false, errors.New("failed to fetch image")
	}
	defer sourceFile.Body.Close()
	file, err := os.Create(user.ID)
	if err != nil {
		return false, errors.New("failed to create file")
	}
	_, err = io.Copy(file, sourceFile.Body)
	if err != nil {
		return false, errors.New("failed to copy file")
	}
	defer os.Remove(file.Name())
	file.Close()
	src, err := pigo.GetImage(file.Name())
	if err != nil {
		return false, errors.New("failed to GetImage")
	}
	pixels := pigo.RgbToGrayscale(src)
	cols, rows := src.Bounds().Max.X, src.Bounds().Max.Y
	cParams := pigo.CascadeParams{
		MinSize:     *minSize,
		MaxSize:     *maxSize,
		ShiftFactor: *shiftFactor,
		ScaleFactor: *scaleFactor,
	}
	imgParams := pigo.ImageParams{
		Pixels: pixels,
		Rows:   rows,
		Cols:   cols,
		Dim:    cols,
	}
	picture := pigo.NewPigo()
	// Unpack the binary file. This will return the number of cascade trees,
	// the tree depth, the threshold and the prediction from tree's leaf nodes.
	classifier, err := picture.Unpack(cascadeFile)
	if err != nil {
		return false, errors.New("failed to unpack the cascade file")
	}
	// Run the classifier over the obtained leaf nodes and return the detection results.
	// The result contains quadruplets representing the row, column, scale and detection score.
	details := classifier.RunCascade(imgParams, cParams)
	if len(details) > 0 {
		return true, nil
	}
	return false, nil
}

func filterUsers() {
	temp, _ := api.GetUsers()
	for _, user := range temp {
		goodFace, err := facePresent(&user)
		if err != nil {
			fmt.Println(err)
		}
		if user.IsBot || user.Deleted || user.IsRestricted || !goodFace {
			continue
		} else {
			if !containsDuplicates(user, directory) {
				directory = append(directory, user)
			}
		}
	}
}

//func scoreBoard(rtm *slack.RTM, msg *slack.MessageEvent, user *Player) {
//	if (user.correct - user.incorrect) > (first.correct - first.incorrect) {
//		first = user
//	} else if (user.correct - user.incorrect) > (second.correct - second.incorrect) {
//		second = user
//	} else if (user.correct - user.incorrect) > (third.correct - third.incorrect) {
//		third = user
//	}
//
//	response := "top scores (correct - incorrect) :\n1. " + strconv.Itoa(first.correct-first.incorrect) +
//		"\n2. " + strconv.Itoa(second.correct-second.incorrect) +
//		"\n3. " + strconv.Itoa(third.correct-third.incorrect)
//	rtm.SendMessage(rtm.NewOutgoingMessage(response, msg.Channel))
//}
func sendBio(rtm *slack.RTM, msg *slack.MessageEvent, user slack.User) {
	var params slack.PostMessageParameters
	var slice []slack.Attachment
	slice = append(slice, slack.Attachment{
		Color:    "f9dc1b",
		Title:    user.RealName,
		Text:     user.Profile.Email + "\n" + user.Profile.Phone + "\nslack username: " + user.Profile.DisplayName + "\n",
		ImageURL: user.Profile.Image192,
	})
	params.Attachments = slice
	rtm.PostMessage(msg.Channel, "", params)
}

//func sendNotifications(rtm *slack.RTM, msg *slack.MessageEvent) {
//	ticker := time.NewTicker(time.Hour)
//	go func() {
//		for t := range ticker.C {
//			fmt.Println("Tick at ", t)
//			sendBio(rtm, msg, directory[rand.Intn(len(directory))])
//		}
//	}()
//
//	time.Sleep(time.Hour * 4)
//	ticker.Stop()
//	defer rtm.SendMessage(rtm.NewOutgoingMessage("this is your last notification. type and enter 'notify me' if you'd like to continue.", msg.Channel))
//}