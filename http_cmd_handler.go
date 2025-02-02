package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/nu7hatch/gouuid"
)

type RunCmdReq struct {
	Cmd   string   `json:"cmd"`
	Async bool     `json:"async,omitempty"`
	Dir   string   `json:"dir,omitempty"`
	Env   []string `json:"env,omitempty"`
}

type QueryCmdRes Job
type SyncRunCmdRes Job
type AsyncRuncmdRes struct {
	Id         string    `json:"id"`
	CreateTime time.Time `json:"create_time"`
}

var (
	gJobBookkeeper *JobBookkeeper
)

func init() {
	gHttpServer.AddToInit(InitCmdHandler)
	gHttpServer.AddToUninit(UninitCmdHandler)
}

func InitCmdHandler() error {
	gJobBookkeeper = NewJobBookkeeper(gApp.Cnf.ExpireDays)
	return nil
}

func UninitCmdHandler() {
	gJobBookkeeper.Close()
}

func RunCmdHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var req RunCmdReq
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorf("failed to read r.Body: %s", err)
		ServeJSON(w, NewResponse().SetError(ECUnknown, "failed to read body"))
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		log.Errorf("failed to unmarshall data: %s body:%s", err, body)
		ServeJSON(w, NewResponse().SetError(ECUnknown, "failed to unmarshall data"))
		return
	}

	if req.Cmd == "" {
		ServeJSON(w, NewResponse().SetError(ECInvalidParam, "param cmd is empty"))
		return
	}

	var job Job
	job.Cmd = req.Cmd
	job.Dir = req.Dir
	job.Env = req.Env
	job.Status = JSRunning
	job.CreateTime = time.Now()
	job.FinishTime = time.Unix(0, 0)

	u4, err := uuid.NewV4()
	if err != nil {
		log.Errorf("failed to genereate uuid: %s", err)
		ServeJSON(w, NewResponse().SetError(ECUnknown, "failed to generate uuid"))
		return
	}
	job.Id = u4.String()

	ctx, cancel := context.WithCancel(context.Background())
	job.cancelFunc = cancel

	gJobBookkeeper.Add(&job)

	var resp interface{}
	if !req.Async {
		cmdWorker(ctx, &job)
		resp = (*SyncRunCmdRes)(&job)
	} else {
		go cmdWorker(ctx, &job)
		resp = &AsyncRuncmdRes{
			Id:         job.Id,
			CreateTime: job.CreateTime,
		}
	}
	ServeJSON(w, NewResponse().SetData(resp))

}

func cmdWorker(ctx context.Context, job *Job) {
	var err error
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	defer func() {
		job.FinishTime = time.Now()
		job.Stdout = stdout.String()
		job.Stderr = stderr.String()
	}()

	//arch:amd64 os:windows
	goarch := runtime.GOARCH
	goos := runtime.GOOS

	var cmd *exec.Cmd
	if goos == "windows" {
		cmd = exec.Command("cmd", "/c", job.Cmd)
	} else {
		cmd = exec.Command("sh", "-c", job.Cmd)
	}

	cmd.Dir = job.Dir
	cmd.Env = append(cmd.Env, job.Env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Infof("running cmd: %s, job id: %s arch:%s os:%s", job.Cmd, job.Id, goarch, goos)
	err = cmd.Start()
	if err != nil {
		log.Errorf("cmd.Start failed: %s", err)
		job.Error = err.Error()
		job.Status = JSFailed
		return
	}

	job.Pid = cmd.Process.Pid

	doneC := make(chan struct{})
	canceled := false
	// Wait for context cancel
	go func() {
		select {
		case <-ctx.Done():
			canceled = true
			cmd.Process.Kill()
			log.Info("canceling the process: ", job.Id)
		case <-doneC:
		}
	}()

	// Wait until the process exits or be killed
	err = cmd.Wait()
	close(doneC)
	if err != nil {
		// The process has been killed, exit with non-zero, or termiated by some signal
		log.Error("c.Process.Wait failed: ", err)

		if ee, ok := err.(*exec.ExitError); ok && ee.Exited() {
			exitCode := ee.Sys().(syscall.WaitStatus).ExitStatus()
			log.Error("process exited with non-zero exit code: ", exitCode)
			job.ExitCode = exitCode
		}

		job.Error = err.Error()
		job.Status = JSFailed

	} else {
		log.Info("process finished: ", job.Id)
		job.Status = JSFinished
	}

	// If has been canceled by user
	if canceled {
		log.Warn("process canceled: ", job.Id)
		job.Error = err.Error()
		job.Status = JSCanceled
	}

}

// Handler to query the job info by job id
func QueryCmdHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		ServeJSON(w, NewResponse().SetError(ECInvalidParam, "param id is empty"))
		return
	}
	job := gJobBookkeeper.Get(id)
	if job == nil {
		ServeJSON(w, NewResponse().SetError(ECJobNotFound, "job not found: "+id))
		return
	}
	resp := (*QueryCmdRes)(job)
	ServeJSON(w, NewResponse().SetData(resp))

}

func ListCmdHandler(w http.ResponseWriter, r *http.Request) {
	jobs := gJobBookkeeper.GetAll()
	ServeJSON(w, NewResponse().SetData(jobs))
}

// Handler to cancel the job by job id
func CancelCmdHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.FormValue("id"))
	job := gJobBookkeeper.Get(id)
	if job == nil {
		ServeJSON(w, NewResponse().SetError(ECJobNotFound, "job not found: "+id))
		return
	}
	if job.Status != JSRunning {
		ServeJSON(w, NewResponse().SetError(ECJobNotRunning, "job is not running: "+id))
		return
	}
	// Cancel the job
	job.cancelFunc()
	ServeJSON(w, NewResponse())
	return
}
