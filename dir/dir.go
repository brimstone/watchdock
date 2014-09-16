package dir

import (
	"encoding/json"
	"gopkg.in/fsnotify.v1"
	"io/ioutil"
	"log"
	"os"
)

type Dir struct {
	directory string
	watcher   *fsnotify.Watcher
	objects   []map[string]interface{}
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
	return err
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

func (dir *Dir) scandir(channel chan map[string]interface{}) error {
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
		obj, err := dir.validate(dir.directory + "/" + file.Name())
		if err != nil {
			log.Printf("Found invalid json file: %s\n", file.Name())
			continue
		}
		log.Printf("Found valid json file: %s\n", file.Name())
		dir.objects = append(dir.objects, obj)
		channel <- obj
	}
	return nil
}

func (dir *Dir) Sync(channel chan map[string]interface{}) {
	defer dir.watcher.Close()

	dir.scandir(channel)

	// basically run forever
	for {
		select {
		// when we get a modified file

		case event := <-dir.watcher.Events:

			if event.Op&fsnotify.Write == fsnotify.Write {
				obj, err := dir.validate(event.Name)
				// todo - add a check of modification times to debounce
				if err == nil {
					log.Printf("Detected change in %s\n", obj["name"])
					log.Println(event)
					channel <- obj
				}

			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				// todo - channel <- channel.File{Filename: event.Name}
			}

		// Error
		case err := <-dir.watcher.Errors:
			log.Println("Dir error:", err)
		// when we get a new container, write it to disk
		case fileMap := <-channel:
			log.Println("Got notification about:", fileMap)
			rawJson, err := json.Marshal(fileMap)
			if err != nil {
				log.Println("Got an error Marshalling:", err.Error())
				continue
			}
			// todo - log our own write so we don't trigger later
			fo, err := os.Create("/tmp/containers/blah.json")
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
