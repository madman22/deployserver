package main

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"io"
	"path/filepath"
	"strconv"

	//"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	//bolt "go.etcd.io/bbolt"
	"github.com/asdine/storm"

	"github.com/gorilla/mux"
)

//TODO implement archiving in web ui
//TODO fix delete files

var ErrorNotImplemented = errors.New("Not Implemented")

var templates = template.Must(template.ParseFiles("index.html", "service.txt"))

func main() {

	//db, err := bolt.Open("db.db", 0666, nil)
	db, err := storm.Open("db.db")
	if err != nil {
		panic(err.Error())
	}

	errchan := make(chan error, 100)
	go func() {
		for {
			select {
			case err := <-errchan:
				log.Println(err.Error())
			}
		}
	}()

	m := NewManager(db, errchan)

	port, err := m.GetDatabasePort()
	//if err := db.GetValue("port", &port); err != nil {
	if err != nil {
		port = 8181
		m.SetDatabasePort(port)
	}

	r := mux.NewRouter()

	r.HandleFunc("/", m.showIndex)
	r.HandleFunc("/upload", m.handleUpload)
	r.HandleFunc("/update/{id}", m.handleUpdate)
	r.HandleFunc("/start/{id}", m.handleStart)
	r.HandleFunc("/stop/{id}", m.handleStop)
	r.HandleFunc("/enable/{id}", m.handleEnable)
	r.HandleFunc("/disable/{id}", m.handleDisable)
	r.HandleFunc("/remove/{id}", m.handleRemove)
	r.HandleFunc("/reload", m.handleReload)
	r.HandleFunc("/files/{id}", m.handleFiles)
	r.HandleFunc("/delete/{id}/{file}", m.handleDelete)

	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(port), r); err != nil {
			errchan <- err
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-signalChan:
		if err := db.Close(); err != nil {
			log.Println("Database Close", err.Error())
		}
		time.Sleep(1 * time.Second)
		return
	}
}

type Service struct {
	ID        string
	Location  string
	Timestamp time.Time
}

type Manager struct {
	servmux  sync.RWMutex
	services map[string]Service
	db       *storm.DB
	errchan  chan error
}

type Template struct {
	Body string
}

func NewManager(db *storm.DB, errchan chan error) *Manager {
	var m Manager
	m.db = db
	m.services = make(map[string]Service)
	m.errchan = errchan
	m.loadServices()

	return &m
}

func (m *Manager) loadServices() {
	m.servmux.Lock()
	defer m.servmux.Unlock()
	var servs []Service
	if err := m.db.All(&servs); err == nil {
		if len(servs) > 0 {
			for _, serv := range servs {
				m.services[serv.ID] = serv
			}
		}
	}
}

func (m *Manager) GetDatabasePort() (int, error) {
	var port int
	if err := m.db.Get("settings", "port", &port); err != nil {
		return 0, err
	}
	return port, nil
}

func (m *Manager) SetDatabasePort(port int) error {
	if err := m.db.Set("settings", "port", &port); err != nil {
		return err
	}
	return nil
}

func (m *Manager) showIndex(w http.ResponseWriter, r *http.Request) {

	var t Template

	/*var servics []Service
	if err := m.db.GetAll(&services); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}*/

	m.servmux.RLock()
	defer m.servmux.RUnlock()

	if len(m.services) < 1 {
		t.Body = `No Services.  Upload New Service!`
	} else {
		t.Body += `<table><thead><tr><th>Name</th><th>Status</th><th>AutoStart</th><th>Start/Stop</th><th>Update</th><th>Files</th><th>Remove</th></tr></thead><tbody>`
		for _, service := range m.services {
			t.Body += `<tr>`
			t.Body += `<td>` + service.ID + `</td>`
			t.Body += `<td>` + strings.Replace(m.getStatus(service.ID), "\n", "<br>", -1) + `</td>`

			if m.isEnabled(service.ID) {
				t.Body += `<td>Enabled<small><a href="/disable/` + service.ID + `">(disable)</a><big></td>`
				//t.Body += `<td><input type="checkbox" onchange="changeAuto('`+service.ID+`')" id="` + service.ID + `-auto" checked></td>`
			} else {
				t.Body += `<td>Disabled<small><a href="/enable/` + service.ID + `">(enable)</a><big></td>`
				//t.Body += `<td><input type="checkbox" onchange="changeAuto('`+service.ID+`')" id="` + service.ID + `-auto"></td>`
			}

			if m.isRunning(service.ID) {
				t.Body += `<td><a href="/stop/` + service.ID + `">Stop</a></td>`
			} else {
				t.Body += `<td><a href="/start/` + service.ID + `">Start</a></td>`
			}

			t.Body += `<td><form action="/update/` + service.ID + `" method="post" enctype="multipart/form-data"><input type="file" id="file" name="file"><input type="submit" class="waves-effect waves-light btn" value="Update"></form>
			<br>Last Update:` + service.Timestamp.Format(time.Stamp) + `</td>`
			t.Body += `<td><a href="/files/` + service.ID + `">Files</a></td>`
			t.Body += `<td><a href="/remove/` + service.ID + `">Remove</a></td>`
			t.Body += `</tr>`
		}
		t.Body += `</tbody></table>`
	}

	t.Body += `
	<div class="card-panel">
	<a href="/reload">Reload Daemons</a><br><br>Upload:
	<form action="/upload" method="post" enctype="multipart/form-data">
	<input type="file" id="file" name="file">
	<input type="submit" class="waves-effect waves-light btn" value="Upload">
	</form></div>
	`

	if err := templates.ExecuteTemplate(w, "index.html", &t); err != nil {
		log.Println(err.Error())
	}
}

func (m *Manager) check(name string) bool {
	m.servmux.RLock()
	defer m.servmux.RUnlock()
	if _, ok := m.services[name]; ok {
		return true
	}
	return false
}

func (m *Manager) isEnabled(name string) bool {
	if !m.check(name) {
		return false
	}
	//command := "systemctl is-enabled " + name
	cmd := exec.Command("systemctl", "is-enabled", name)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	if !strings.Contains(string(out), "enabled") {
		return false
	}
	return true
}

func (m *Manager) isRunning(name string) bool {
	if !m.check(name) {
		return false
	}
	//command := "systemctl is-active " + name
	cmd := exec.Command("systemctl", "is-active", name)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	if !strings.Contains(string(out), "active") {
		return false
	}
	return true
}

func (m *Manager) start(name string) error {
	if !m.check(name) {
		return errors.New("Service is not managed")
	}
	//command := "systemctl start " + name
	cmd := exec.Command("systemctl", "start", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func (m *Manager) stop(name string) error {
	if !m.check(name) {
		return errors.New("Service is not managed")
	}
	//command := "systemctl stop " + name
	cmd := exec.Command("systemctl", "stop", name)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func (m *Manager) enable(name string) error {
	if !m.check(name) {
		return errors.New("Service is not managed")
	}
	//command := "systemctl enable " + name
	cmd := exec.Command("systemctl", "enable", name)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func (m *Manager) disable(name string) error {
	if !m.check(name) {
		return errors.New("Service is not managed")
	}
	//command := "systemctl disable " + name
	cmd := exec.Command("systemctl", "disable", name)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func (m *Manager) getStatus(name string) string {
	if !m.check(name) {
		return errors.New("Service is not managed").Error()
	}
	//command := "systemctl status " + name
	cmd := exec.Command("systemctl", "status", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err.Error()
	}
	return string(out)
}

func (m *Manager) makeExecutable(name string) error {
	if !m.check(name) {
		return errors.New("Service is not managed")
	}

	cmd := exec.Command("chmod", "+x", "/opt/deployserver/services/"+name+"/"+name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func (m *Manager) reloadDaemons() error {
	cmd := exec.Command("systemctl", "daemon-reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New(string(out))
	}
	return nil
}

func getID(r *http.Request) (string, error) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		return "", errors.New("Missing ID")
	}
	return id, nil
}

func (m *Manager) handleStart(w http.ResponseWriter, r *http.Request) {
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.start(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleStop(w http.ResponseWriter, r *http.Request) {
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.stop(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleEnable(w http.ResponseWriter, r *http.Request) {
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.enable(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleDisable(w http.ResponseWriter, r *http.Request) {
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := m.disable(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	var s Service

	if !strings.Contains(header.Filename, ".zip") {
		http.Error(w, "Wrong File", http.StatusBadRequest)
		return
	}

	s.ID = strings.TrimSuffix(header.Filename, ".zip")
	s.ID = strings.Replace(s.ID, " ", "", -1)

	if m.check(s.ID) {
		if m.isRunning(s.ID) {
			if err := m.stop(s.ID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if err := m.archive(s.ID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.RemoveAll("services/" + s.ID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	rdr, err := zip.NewReader(file, header.Size)
	if err != nil {
		http.Error(w, "Invalid Zip File", http.StatusBadRequest)
		return
	}
	if len(rdr.File) < 1 {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll("services/"+s.ID, os.ModePerm); err != nil {
		http.Error(w, "Invalid Zip File", http.StatusBadRequest)
		return
	}
	s.Timestamp = time.Now()
	for _, f := range rdr.File {
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll("services/"+s.ID+"/"+f.Name, f.Mode()); err != nil {
				log.Println(err.Error())
				continue
			}
		} else {
			rc, err := f.Open()
			if err != nil {
				log.Println(err.Error())
				continue
			}
			///os.MkdirAll("services/"+s.ID+"/"+f.Name, f.Mode())
			nf, err := os.OpenFile("services/"+s.ID+"/"+f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				rc.Close()
				log.Println(err.Error())
				continue
			}
			io.Copy(nf, rc)
			nf.Close()
			rc.Close()
		}
	}
	if err := m.createService(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.Chmod("services/"+s.ID+"/"+s.ID, 777); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.servmux.Lock()
	m.services[s.ID] = s
	m.servmux.Unlock()

	if err := m.makeExecutable(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := m.db.Save(&s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleUpdate(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !m.check(id) {
		http.Error(w, "Service Not Managed", http.StatusInternalServerError)
		return
	}

	running := m.isRunning(id)
	if running {
		if err := m.stop(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := os.Stat("services/" + id + "/" + header.Filename); err == nil {
		if err := os.Remove("services/" + id + "/" + header.Filename); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	newfile, err := os.OpenFile("services/"+id+"/"+header.Filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		file.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(newfile, file)
	if err != nil {
		newfile.Close()
		file.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newfile.Close()
	file.Close()

	if header.Filename == id {
		if err := m.makeExecutable(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	m.WasUpdated(id)

	if running {
		if err := m.start(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleRemove(w http.ResponseWriter, r *http.Request) {
	id, err := getID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !m.check(id) {
		http.Error(w, "Service Not Managed", http.StatusInternalServerError)
		return
	}
	if m.isRunning(id) {
		if err := m.stop(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if m.isEnabled(id) {
		if err := m.disable(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if _, err := os.Stat("/lib/systemd/system/" + id + ".service"); err != nil {
		http.Error(w, "Service File not Found", http.StatusInternalServerError)
		return
	}

	if err := os.Remove("/lib/systemd/system/" + id + ".service"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.servmux.Lock()
	delete(m.services, id)
	m.servmux.Unlock()

	var s Service
	if err := m.db.Get("ID", id, &s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := m.db.DeleteStruct(&s); err != nil {
		http.Error(w, err.Error(), http.StatusInsufficientStorage)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := m.reloadDaemons(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) createService(id string) error {

	file, err := os.OpenFile("/lib/systemd/system/"+id+".service", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer file.Close()

	var s Service
	s.ID = id
	s.Location = "/opt/deployserver/services/" + id

	if err := templates.ExecuteTemplate(file, "service.txt", &s); err != nil {
		return err
	}

	return nil
}

func (m *Manager) archive(name string) error {

	os.MkdirAll("backups/"+name, 0777)
	zp, err := os.OpenFile("backups/"+name+"/"+time.Now().Format(time.RFC3339), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer zp.Close()

	wr := zip.NewWriter(zp)
	defer wr.Close()

	info, err := os.Stat("services/" + name)
	if err != nil {
		return err
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base("services/" + name)
	}

	filepath.Walk("services/"+name, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		if baseDir != "" {
			header.Name = filepath.Join(baseDir, strings.TrimPrefix(path, "services/"+name))
		}
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := wr.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return nil
	})

	return nil
}

func (m *Manager) WasUpdated(id string) {
	m.servmux.Lock()
	defer m.servmux.Unlock()
	if serv, ok := m.services[id]; !ok {
		return
	} else {
		serv.Timestamp = time.Now()
		m.services[id] = serv
		if err := m.db.Save(&serv); err != nil {
			m.errchan <- err
		}
	}
}

func (m *Manager) handleFiles(w http.ResponseWriter, r *http.Request) {
	//TODO show files with actions to delete/rename
	var t Template
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}
	m.servmux.RLock()
	defer m.servmux.RUnlock()
	service, ok := m.services[id]
	if !ok {
		http.Error(w, "Missing service", http.StatusBadRequest)
		return
	}
	//t.Body += service.ID
	t.Body += `<div class="row"><div class="col s10"><div class="card"><div class="card-content"><span class="card-title">` + service.ID + `</span>`
	t.Body += `<table><thead><tr><th>Filename</th><th>Remove</th></tr></thead><tbody>`

	if err := filepath.Walk("services/"+id, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil

		}
		t.Body += `<tr>`
		t.Body += `<td>` + info.Name() + `</td>`
		t.Body += `<td><a href="/delete/` + service.ID + `/` + base64.RawURLEncoding.EncodeToString([]byte(info.Name())) + `">Delete</a></td>`
		t.Body += `</tr>`
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t.Body += `</tbody></table>`

	if err := templates.ExecuteTemplate(w, "index.html", &t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	//http.Error(w, NotImplemented.Error(), http.StatusInternalServerError)
}

func (m *Manager) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, ok := vars["id"]
	if !ok {
		http.Error(w, "Missing ID", http.StatusBadRequest)
		return
	}
	file, ok := vars["file"]
	if !ok {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}
	out, err := base64.RawURLEncoding.DecodeString(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(out) < 1 {
		http.Error(w, "Missing name", http.StatusInternalServerError)
		return
	}
	filename := "services/" + id + "/" + string(out)

	if _, err := os.Stat(filename); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Remove(filename); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/files/"+id, http.StatusFound)
}
