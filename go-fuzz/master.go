package main

import (
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/rpc"
	"path/filepath"
	"sync"
	"time"
)

type Master struct {
	sync.Mutex
	idSeq  int
	slaves map[int]*MasterSlave
	corpus *PersistentSet
	fresh  *PersistentSet
	known  *PersistentSet
	bugs   *PersistentSet
}

type MasterSlave struct {
	id       int
	pending  []MasterInput
	smashing Artifact
	lastSync time.Time
}

func masterMain(ln net.Listener) {
	m := &Master{}

	m.fresh = newPersistentSet(filepath.Join(*flagWorkdir, "fresh"))
	m.known = newPersistentSet(filepath.Join(*flagWorkdir, "known"))
	m.bugs = newPersistentSet(filepath.Join(*flagWorkdir, "bugs"))
	m.corpus = newPersistentSet(filepath.Join(*flagWorkdir, "corpus"))
	m.corpus.readInDir(*flagCorpus, func(a Artifact) {
		m.fresh.add(a)
	})
	if len(m.corpus.m) == 0 {
		m.corpus.add(Artifact{[]byte{}, 0})
	}

	log.Printf("corpus contains %v inputs, know %v bugs\n", len(m.corpus.m), len(m.known.m))

	m.slaves = make(map[int]*MasterSlave)
	go m.loop()

	s := rpc.NewServer()
	s.Register(m)
	s.Accept(ln)
}

func (m *Master) loop() {
	for range time.NewTicker(syncPeriod).C {
		m.Lock()
		for id, s := range m.slaves {
			if time.Since(s.lastSync) < syncDeadline {
				continue
			}
			log.Printf("slave %v died", s.id)
			delete(m.slaves, id)
			if s.smashing.data != nil {
				// The slave was smashing a new input.
				// Add the input back to the fresh list,
				// so that another slave can pick it up.
				m.fresh.add(s.smashing)
			}
		}
		m.Unlock()
	}
}

type ConnectArgs struct {
}

type ConnectRes struct {
	ID     int
	Corpus []MasterInput
}

type MasterInput struct {
	Data []byte
	Prio uint64
}

func (m *Master) Connect(a *ConnectArgs, r *ConnectRes) error {
	m.Lock()
	defer m.Unlock()

	m.idSeq++
	s := &MasterSlave{id: m.idSeq}
	s.lastSync = time.Now()
	m.slaves[s.id] = s

	r.ID = s.id
	for _, a := range m.corpus.m {
		r.Corpus = append(r.Corpus, MasterInput{a.data, a.meta})
	}
	return nil
}

type NewInputArgs struct {
	Input MasterInput
}

func (m *Master) NewInput(a *NewInputArgs, r *int) error {
	m.Lock()
	defer m.Unlock()

	art := Artifact{a.Input.Data, a.Input.Prio}
	if !m.corpus.add(art) {
		return nil
	}
	m.fresh.add(art)
	for _, s := range m.slaves {
		s.pending = append(s.pending, a.Input)
	}

	data := []byte(a.Input.Data)
	if len(data) > 50 {
		data = data[:50]
	}
	log.Printf("NewInput: [%v]%q", len(a.Input.Data), data)

	return nil
}

type NewBugArgs struct {
	Data  []byte
	Error []byte
}

func (m *Master) NewBug(a *NewBugArgs, r *int) error {
	m.Lock()
	defer m.Unlock()

	supp := extractSuppression(a.Error)
	if !m.known.add(Artifact{supp, 0}) || !m.bugs.add(Artifact{a.Data, 0}) {
		return nil
	}
	m.bugs.addDescription(a.Data, a.Error, "output")

	log.Printf("Failed with '%s' on [%v]%s", a.Error, len(a.Data), hex.EncodeToString(a.Data))

	return nil
}

type SyncArgs struct {
	ID         int
	CorpusSize int
	Execs      uint64
	Coverage   float64
}

type SyncRes struct {
	Inputs []MasterInput
}

func (m *Master) Sync(a *SyncArgs, r *SyncRes) error {
	m.Lock()
	defer m.Unlock()

	//log.Printf("Ping from %v: corpus=%v cov=%.4f execs=%v", a.Id, a.CorpusSize, a.Coverage*100, a.Execs)
	s := m.slaves[a.ID]
	if s == nil {
		return errors.New("unknown slave")
	}
	s.lastSync = time.Now()
	r.Inputs = s.pending
	s.pending = nil
	return nil
}

func extractSuppression(s []byte) []byte {
	return s
}