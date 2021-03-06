package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/go-gorp/gorp"

	"github.com/gorilla/mux"
	"github.com/ovh/cds/engine/api/application"
	"github.com/ovh/cds/engine/api/context"
	"github.com/ovh/cds/engine/api/pipeline"
	"github.com/ovh/cds/engine/api/poller"
	"github.com/ovh/cds/engine/api/repositoriesmanager"
	"github.com/ovh/cds/engine/log"
	"github.com/ovh/cds/sdk"
)

func addPollerHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	projectKey := vars["key"]
	appName := vars["permApplicationName"]
	pipName := vars["permPipelineKey"]

	//Load the application
	app, err := application.LoadApplicationByName(db, projectKey, appName)
	if err != nil {
		WriteError(w, r, sdk.ErrApplicationNotFound)
		return
	}

	app.RepositoryPollers, err = poller.LoadPollersByApplication(db, app.ID)
	if err != nil {
		log.Warning("addPollerHandler> Cannot load pollers for application %s: %s\n", app.Name, err)
		WriteError(w, r, err)
		return
	}

	// Load pipeline
	pip, err := pipeline.LoadPipeline(db, projectKey, pipName, false)
	if err != nil {
		log.Warning("addPollerHandler> Cannot load pipeline: %s\n", err)
		WriteError(w, r, err)
		return
	}

	// Get body
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Warning("addPollerHandler: Cannot read body: %s\n", err)
		WriteError(w, r, err)
		return
	}

	var h sdk.RepositoryPoller
	err = json.Unmarshal(data, &h)
	if err != nil {
		log.Warning("addPollerHandler: Cannot unmarshal body: %s\n", err)
		WriteError(w, r, err)
		return
	}

	h.Application = *app
	h.Pipeline = *pip
	h.Enabled = true

	//Check it the application is attached to a repository
	if app.RepositoriesManager == nil {
		WriteError(w, r, sdk.ErrNoReposManagerClientAuth)
		return
	}

	b, e := repositoriesmanager.CheckApplicationIsAttached(db, app.RepositoriesManager.Name, projectKey, appName)
	if e != nil {
		log.Warning("addPollerHandler> Cannot check app (%s,%s,%s): %s", app.RepositoriesManager.Name, projectKey, appName, e)
		WriteError(w, r, e)
		return
	}

	if !b && app.RepositoryFullname == "" {
		WriteError(w, r, sdk.ErrNoReposManagerClientAuth)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Warning("addPollerHandler> Cannot start transaction: %s", e)
		WriteError(w, r, e)
		return
	}
	defer tx.Rollback()

	// Insert poller in database
	err = poller.InsertPoller(db, &h)
	if err != nil {
		log.Warning("addPollerHandler: cannot insert poller in db: %s\n", err)
		WriteError(w, r, err)
		return
	}

	err = application.UpdateLastModified(tx, app)
	if err != nil {
		log.Warning("addPollerHandler: cannot update application (%s) lastmodified date: %s\n", app.Name, err)
		WriteError(w, r, err)
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Warning("addPollerHandler> Cannot commit transaction: %s", e)
		WriteError(w, r, e)
		return
	}

	app.RepositoryPollers = append(app.RepositoryPollers, h)

	WriteJSON(w, r, app, http.StatusOK)
}

func updatePollerHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	projectKey := vars["key"]
	appName := vars["permApplicationName"]
	pipName := vars["permPipelineKey"]

	//Load the application
	app, err := application.LoadApplicationByName(db, projectKey, appName)
	if err != nil {
		WriteError(w, r, sdk.ErrApplicationNotFound)
		return
	}

	// Load pipeline
	pip, err := pipeline.LoadPipeline(db, projectKey, pipName, false)
	if err != nil {
		log.Warning("updatePollerHandler> Cannot load pipeline: %s\n", err)
		WriteError(w, r, err)
		return
	}

	// Get body
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Warning("updatePollerHandler: Cannot read body: %s\n", err)
		WriteError(w, r, err)
		return
	}

	var h sdk.RepositoryPoller
	err = json.Unmarshal(data, &h)
	if err != nil {
		log.Warning("updatePollerHandler: Cannot unmarshal body: %s\n", err)
		WriteError(w, r, err)
		return
	}

	h.Application = *app
	h.Pipeline = *pip

	tx, err := db.Begin()
	if err != nil {
		log.Warning("updatePollerHandler> cannot start transaction: %s\n", err)
		WriteError(w, r, err)
		return
	}
	defer tx.Rollback()

	// Update poller in database
	err = poller.UpdatePoller(tx, &h)
	if err != nil {
		log.Warning("updatePollerHandler: cannot update poller in db: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err = application.UpdateLastModified(tx, app); err != nil {
		log.Warning("updatePollerHandler: cannot update application last modified date: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err = tx.Commit(); err != nil {
		log.Warning("updatePollerHandler> cannot commit transaction: %s\n", err)
		WriteError(w, r, err)
		return
	}

	app.RepositoryPollers, err = poller.LoadPollersByApplication(db, app.ID)
	if err != nil {
		log.Warning("deleteHook> cannot load pollers: %s\n", err)
		WriteError(w, r, err)
		return
	}

	WriteJSON(w, r, app, http.StatusOK)
}

func getApplicationPollersHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	projectName := vars["key"]
	appName := vars["permApplicationName"]

	a, err := application.LoadApplicationByName(db, projectName, appName)
	if err != nil {
		log.Warning("getApplicationHooksHandler> cannot load application %s/%s: %s\n", projectName, appName, err)
		WriteError(w, r, err)
		return
	}

	pollers, err := poller.LoadPollersByApplication(db, a.ID)
	if err != nil {
		log.Warning("getApplicationHooksHandler> cannot load pollers: %s\n", err)
		WriteError(w, r, err)
		return
	}

	WriteJSON(w, r, pollers, http.StatusOK)
}

func getPollersHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	projectName := vars["key"]
	appName := vars["permApplicationName"]
	pipelineName := vars["permPipelineKey"]

	p, err := pipeline.LoadPipeline(db, projectName, pipelineName, false)
	if err != nil {
		if err != sdk.ErrPipelineNotFound {
			log.Warning("getPollersHandler> cannot load pipeline %s/%s: %s\n", projectName, pipelineName, err)
		}
		WriteError(w, r, err)
		return
	}

	a, err := application.LoadApplicationByName(db, projectName, appName)
	if err != nil {
		log.Warning("getPollersHandler> cannot load application %s/%s: %s\n", projectName, appName, err)
		WriteError(w, r, err)
		return
	}

	poller, err := poller.LoadPollerByApplicationAndPipeline(db, a.ID, p.ID)
	if err != nil {
		log.Warning("getPollersHandler> cannot load poller with ID %d %d: %s\n", p.ID, a.ID, err)
		WriteError(w, r, err)
		return
	}

	WriteJSON(w, r, poller, http.StatusOK)
}

func deletePollerHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	projectName := vars["key"]
	appName := vars["permApplicationName"]
	pipelineName := vars["permPipelineKey"]

	p, err := pipeline.LoadPipeline(db, projectName, pipelineName, false)
	if err != nil {
		if err != sdk.ErrPipelineNotFound {
			log.Warning("getPollersHandler> cannot load pipeline %s/%s: %s\n", projectName, pipelineName, err)
		}
		WriteError(w, r, err)
		return
	}

	a, err := application.LoadApplicationByName(db, projectName, appName)
	if err != nil {
		log.Warning("getPollersHandler> cannot load application %s/%s: %s\n", projectName, appName, err)
		WriteError(w, r, err)
		return
	}

	po, err := poller.LoadPollerByApplicationAndPipeline(db, a.ID, p.ID)
	if err != nil {
		log.Warning("getPollersHandler> cannot load poller: %s\n", err)
		WriteError(w, r, err)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Warning("deleteHook> cannot start transaction: %s\n", err)
		WriteError(w, r, err)
		return
	}
	defer tx.Rollback()

	if err = poller.DeletePoller(tx, po); err != nil {
		log.Warning("deleteHook> cannot delete poller: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err = application.UpdateLastModified(tx, a); err != nil {
		log.Warning("deleteHook> cannot update application last modified date: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err = tx.Commit(); err != nil {
		log.Warning("deleteHook> cannot commit transaction: %s\n", err)
		WriteError(w, r, err)
		return
	}

	a.RepositoryPollers, err = poller.LoadPollersByApplication(db, a.ID)
	if err != nil {
		log.Warning("deleteHook> cannot load pollers: %s\n", err)
		WriteError(w, r, err)
		return
	}

	WriteJSON(w, r, a, http.StatusOK)
}
