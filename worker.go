package main

import (
    "log"
    "container/list"
    "huabot-sched/db"
    "strconv"
    "bytes"
)


type Worker struct {
    jobQueue *list.List
    conn     Conn
    sched    *Sched
    alive    bool
    Funcs    []string
}


func NewWorker(sched *Sched, conn Conn) (worker *Worker) {
    worker = new(Worker)
    worker.conn = conn
    worker.jobQueue = list.New()
    worker.sched = sched
    worker.Funcs = make([]string, 0)
    worker.alive = true
    return
}


func (worker *Worker) IsAlive() bool {
    return worker.alive
}


func (worker *Worker) HandleDo(job db.Job) (err error){
    log.Printf("HandleDo: %d\n", job.Id)
    worker.jobQueue.PushBack(job)
    pack, err := packJob(job)
    if err != nil {
        log.Printf("Error: packJob %d %s\n", job.Id, err.Error())
        return nil
    }
    err = worker.conn.Send(pack)
    if err != nil {
        return err
    }
    job.Status = "doing"
    job.Save()
    return nil
}


func (worker *Worker) HandleCanDo(Func string) error {
    log.Printf("HandleCanDo: %s\n", Func)
    for _, f := range worker.Funcs {
        if f == Func {
            return nil
        }
    }
    worker.Funcs = append(worker.Funcs, Func)
    worker.sched.AddFunc(Func)
    return nil
}


func (worker *Worker) HandleCanNoDo(Func string) error {
    log.Printf("HandleCanDo: %s\n", Func)
    newFuncs := make([]string, 0)
    for _, f := range worker.Funcs {
        if f == Func {
            continue
        }
        newFuncs = append(newFuncs, f)
    }
    worker.Funcs = newFuncs
    return nil
}


func (worker *Worker) HandleDone(jobId int64) (err error) {
    log.Printf("HandleDone: %d\n", jobId)
    worker.sched.Done(jobId)
    removeListJob(worker.jobQueue, jobId)
    return nil
}


func (worker *Worker) HandleFail(jobId int64) (err error) {
    log.Printf("HandleFail: %d\n", jobId)
    worker.sched.Fail(jobId)
    removeListJob(worker.jobQueue, jobId)
    return nil
}


func (worker *Worker) HandleWaitForJob() (err error) {
    log.Printf("HandleWaitForJob\n")
    err = worker.conn.Send([]byte("wait_for_job"))
    return nil
}


func (worker *Worker) HandleSchedLater(jobId, delay int64) (err error){
    log.Printf("HandleSchedLater: %d %d\n", jobId, delay)
    worker.sched.SchedLater(jobId, delay)
    removeListJob(worker.jobQueue, jobId)
    return nil
}


func (worker *Worker) HandleNoJob() (err error){
    log.Printf("HandleNoJob\n")
    err = worker.conn.Send([]byte("no_job"))
    return
}


func (worker *Worker) HandleGrabJob() (err error){
    log.Printf("HandleGrabJob\n")
    worker.sched.grabQueue.PushBack(worker)
    worker.sched.Notify()
    return nil
}


func (worker *Worker) Handle() {
    var payload []byte
    var err error
    var conn = worker.conn
    for {
        payload, err = conn.Receive()
        if err != nil {
            log.Printf("Error: %s\n", err.Error())
            worker.sched.DieWorker(worker)
            return
        }

        buf := bytes.NewBuffer(nil)
        buf.WriteByte(NULL_CHAR)
        null_char := buf.Bytes()

        parts := bytes.SplitN(payload, null_char, 2)
        cmd := string(parts[0])
        switch cmd {
        case "grab":
            err = worker.HandleGrabJob()
            break
        case "done":
            if len(parts) != 2 {
                log.Printf("Error: invalid format.")
                break
            }
            jobId, _ := strconv.ParseInt(string(parts[1]), 10, 0)
            err = worker.HandleDone(jobId)
            break
        case "fail":
            if len(parts) != 2 {
                log.Printf("Error: invalid format.")
                break
            }
            jobId, _ := strconv.ParseInt(string(parts[1]), 10, 0)
            err = worker.HandleFail(jobId)
            break
        case "sched_later":
            if len(parts) != 2 {
                log.Printf("Error: invalid format.")
                break
            }
            parts = bytes.SplitN(parts[1], null_char, 2)
            if len(parts) != 2 {
                log.Printf("Error: invalid format.")
                break
            }
            jobId, _ := strconv.ParseInt(string(parts[0]), 10, 0)
            delay, _ := strconv.ParseInt(string(parts[1]), 10, 0)
            err = worker.HandleSchedLater(jobId, delay)
            break
        case "sleep":
            err = conn.Send([]byte("nop"))
            break
        case "ping":
            err = conn.Send([]byte("pong"))
            break
        case "can_do":
            err = worker.HandleCanDo(string(parts[1]))
            break
        case "can_no_do":
            err = worker.HandleCanNoDo(string(parts[1]))
            break
        default:
            err = conn.Send([]byte("unknown"))
            break
        }
        if err != nil {
            log.Printf("Error: %s\n", err.Error())
            worker.alive = false
            worker.sched.DieWorker(worker)
            return
        }

        if !worker.alive {
            break
        }
    }
}


func (worker *Worker) Close() {
    worker.conn.Close()
    for e := worker.jobQueue.Front(); e != nil; e = e.Next() {
        worker.sched.Fail(e.Value.(db.Job).Id)
    }
}
