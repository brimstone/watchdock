package dir

import (
	"encoding/json"
	"gopkg.in/fsnotify.v1"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

type Dir struct {
	directory string
	watcher   *fsnotify.Watcher
	modtime   map[string]time.Time
}

func (dir *Dir) Init(directory string) error {
	dir.directory = directory
	var err error
	dir.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		os.Mkdir(directory, 0755)
	}
	err = dir.watcher.Add(dir.directory)
	if err != nil {
		return err
	}

	dir.modtime = make(map[string]time.Time)
	return nil
}

func (dir *Dir) validate(filename string) (map[string]interface{}, error) {
	//temp json object
	var obj map[string]interface{}

	// read in the whole file contents
	fileContents, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Printf("Error reading: %s, %s\n", filename, err.Error())
		return nil, err
	}
	// attempt to convert the file contents into a json object
	err = json.Unmarshal(fileContents, &obj)
	if err != nil {
		log.Printf("Error unmarshalling %s: %s\n", filename, err.Error())
		return nil, err
	}
	return obj, nil
}

func (dir *Dir) scandir(channel chan<- map[string]interface{}) error {
	directory, err := os.Open(dir.directory)
	if err != nil {
		log.Printf("Error opening %s\n", dir.directory)
		return err
	}
	files, err := directory.Readdir(0)
	if err != nil {
		log.Printf("Error reading %s: %s\n", dir.directory, err.Error())
		return err
	}
	for _, file := range files {
		filename := dir.directory + "/" + file.Name()
		obj, err := dir.validate(filename)
		if err != nil {
			log.Printf("Found invalid json file: %s\n", file.Name())
			continue
		}
		log.Printf("Found valid json file: %s\n", file.Name())
		stat, _ := os.Stat(filename)
		dir.modtime[filename] = stat.ModTime()
		channel <- obj
	}
	return nil
}

func (dir *Dir) Sync(readChannel <-chan map[string]interface{}, writeChannel chan<- map[string]interface{}) {
	defer dir.watcher.Close()

	go dir.scandir(writeChannel)

	// basically run forever
	for {
		select {
		// when we get a modified file

		case event := <-dir.watcher.Events:

			if event.Op&fsnotify.Write == fsnotify.Write {
				obj, err := dir.validate(event.Name)
				// todo - add a check of modification times to debounce
				if err == nil {
					log.Printf("Detected change in %s\n", obj["Name"])
					filename := obj["Name"].(string)
					if time.Now().Before(dir.modtime[filename].Add(time.Second)) {
						log.Println("stored time:", dir.modtime[filename])
						continue
					}
					dir.modtime[filename] = time.Now()
					writeChannel <- obj
				}

			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				// todo - channel <- channel.File{Filename: event.Name}
			}

		// Error
		case err := <-dir.watcher.Errors:
			log.Println("Dir error:", err)
		// when we get a new container, write it to disk
		case fileMap := <-readChannel:
			//log.Println("Got notification about:", fileMap)
			rawJson, err := json.Marshal(fileMap)
			if err != nil {
				log.Println("Got an error Marshalling:", err.Error())
				continue
			}
			// todo - log our own write so we don't trigger later
			names := strings.Split(fileMap["Name"].(string), "/")
			log.Println("Writing to ", names[1])
			fo, err := os.Create("/tmp/containers/" + names[1] + ".json")
			if err != nil {
				log.Println("Got an writing:", err.Error())
				continue
			}
			fo.Write(rawJson)
			fo.Close()
		}
	}
}

func New(directory string) (*Dir, error) {
	dir := new(Dir)
	err := dir.Init(directory)
	if err != nil {
		return nil, err
	}
	return dir, nil
}
