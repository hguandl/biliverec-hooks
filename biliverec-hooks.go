package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type RecEvent struct {
	EventType      string    `json:"EventType"`
	EventTimestamp time.Time `json:"EventTimestamp"`
	EventID        string    `json:"EventId"`
	EventData      EventData `json:"EventData"`
}
type EventData struct {
	RelativePath   string    `json:"RelativePath"`
	FileSize       int       `json:"FileSize"`
	Duration       float64   `json:"Duration"`
	FileOpenTime   time.Time `json:"FileOpenTime"`
	FileCloseTime  time.Time `json:"FileCloseTime"`
	SessionID      string    `json:"SessionId"`
	RoomID         int       `json:"RoomId"`
	ShortID        int       `json:"ShortId"`
	Name           string    `json:"Name"`
	Title          string    `json:"Title"`
	AreaNameParent string    `json:"AreaNameParent"`
	AreaNameChild  string    `json:"AreaNameChild"`
}

type StatusData struct {
	Running bool   `json:"running"`
	LastLog string `json:"last_log"`
}

var baseDir *string
var botAPI *string
var logDir *string
var tranQue chan string

func baseName(fileName string) string {
	if pos := strings.LastIndexByte(fileName, '.'); pos != -1 {
		return fileName[:pos]
	}
	return fileName
}

func notifyRoomEvent(api string, roomID int, event string) {
	resp, err := http.PostForm(api,
		url.Values{
			"roomid": {fmt.Sprint(roomID)},
			"event":  {event},
		})
	if err != nil {
		log.Printf("Error: %v.", err)
		return
	}

	defer resp.Body.Close()
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

	var e RecEvent
	err := json.NewDecoder(r.Body).Decode(&e)
	if err != nil {
		http.Error(w, "Cannot parse body", http.StatusBadRequest)
	}

	switch e.EventType {
	case "SessionStarted":
		log.Printf("<%v> online.", e.EventData.RoomID)
		notifyRoomEvent(*botAPI, e.EventData.RoomID, "ONLINE")
	case "FileOpening":
		log.Printf("<%v> \"%v/%v\" created.", e.EventData.RoomID, *baseDir, e.EventData.RelativePath)
		notifyRoomEvent(*botAPI, e.EventData.RoomID, "START")
	case "FileClosed":
		notifyRoomEvent(*botAPI, e.EventData.RoomID, "STOP")
		filename := fmt.Sprintf("%v/%v", *baseDir, e.EventData.RelativePath)
		log.Printf("<%v> \"%v\" finished.", e.EventData.RoomID, filename)
		tranQue <- filename
	case "SessionEnded":
		log.Printf("<%v> offline.", e.EventData.RoomID)
		notifyRoomEvent(*botAPI, e.EventData.RoomID, "OFFLINE")
	}
}

func getStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

	files, err := ioutil.ReadDir(*logDir)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fileName := "bilirec19700101.txt"

	for _, f := range files {
		if !strings.HasPrefix(f.Name(), "bilirec") {
			continue
		}
		if !strings.HasSuffix(f.Name(), ".txt") {
			continue
		}

		if f.Name() > fileName {
			fileName = f.Name()
		}
	}

	content, err := os.Open(path.Join(*logDir, fileName))
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(content)
	var lastLog string

	for scanner.Scan() {
		lastLog = scanner.Text()
	}

	regex := regexp.MustCompile("^{.*\"ProcessId\":\\s*(\\d+),.*}$")
	res := regex.FindAllStringSubmatch(lastLog, -1)

	pid, err := strconv.Atoi(res[0][1])
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	running := true
	process, err := os.FindProcess(pid)
	if err != nil {
		running = false
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		running = false
	}

	statusData := StatusData{
		Running: running,
		LastLog: lastLog,
	}

	respBody, err := json.Marshal(statusData)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.Write(respBody)
}

func transcode() {
	for {
		filename := <-tranQue
		outputName := fmt.Sprintf("%v-hevc.mp4", baseName(filename))
		cmd := exec.Command("ffmpeg",
			"-nostdin",
			"-loglevel", "quiet",
			"-i", filename,
			"-c:v", "libx265",
			"-x265-params", "log-level=none",
			"-pix_fmt", "yuv420p10le",
			"-tag:v", "hvc1",
			"-max_muxing_queue_size", "4096",
			"-c:a", "copy",
			outputName)

		logEnv := fmt.Sprintf("FFREPORT=file=%v/ffmpeg-logs/%%p-%%t.log:level=32", *baseDir)
		cmd.Env = append(cmd.Env, logEnv)
		err := cmd.Run()
		if err != nil {
			log.Printf("Error: %v", err)
		} else {
			log.Printf("Transcode finished: %v", outputName)
		}
	}
}

func main() {
	host := flag.String("h", "", "Host for listen")
	port := flag.Int("p", 8080, "Port for listen")
	baseDir = flag.String("d", ".", "Base directory")
	botAPI = flag.String("b", "http://localhost:8888", "Bot API URL")
	logDir = flag.String("l", ".", "Recorder logs Directory")

	flag.Parse()
	addr := fmt.Sprintf("%v:%v", *host, *port)
	tranQue = make(chan string)

	http.HandleFunc("/", handler)
	http.HandleFunc("/getStatus", getStatus)
	log.Printf("Listening at \"%v\"", addr)

	go transcode()
	log.Fatal(http.ListenAndServe(addr, nil))
}
