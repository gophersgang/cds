package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/go-gorp/gorp"
	"github.com/gorilla/mux"

	"github.com/ovh/cds/engine/api/context"
	"github.com/ovh/cds/engine/api/environment"
	"github.com/ovh/cds/engine/api/project"
	"github.com/ovh/cds/engine/api/sanity"
	"github.com/ovh/cds/engine/api/secret"
	"github.com/ovh/cds/engine/log"
	"github.com/ovh/cds/sdk"
)

func getEnvironmentsAuditHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]

	audits, errAudit := environment.GetEnvironmentAudit(db, key, envName)
	if errAudit != nil {
		log.Warning("getEnvironmentsAuditHandler: Cannot get environment audit for project %s: %s\n", key, errAudit)
		WriteError(w, r, errAudit)
		return
	}
	WriteJSON(w, r, audits, http.StatusOK)
}

func restoreEnvironmentAuditHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]
	auditIDString := vars["auditID"]

	auditID, errAudit := strconv.ParseInt(auditIDString, 10, 64)
	if errAudit != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot parse auditID %s: %s\n", auditIDString, errAudit)
		WriteError(w, r, sdk.ErrInvalidID)
		return
	}

	p, errProj := project.LoadProject(db, key, c.User)
	if errProj != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot load project %s: %s\n", key, errProj)
		WriteError(w, r, errProj)
		return
	}

	env, errEnv := environment.LoadEnvironmentByName(db, key, envName)
	if errEnv != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot load environment %s: %s\n", envName, errEnv)
		WriteError(w, r, errEnv)
		return
	}

	auditVars, errGetAudit := environment.GetAudit(db, auditID)
	if errGetAudit != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot get environment audit for project %s: %s\n", key, errGetAudit)
		WriteError(w, r, errGetAudit)
		return
	}

	tx, errBegin := db.Begin()
	if errBegin != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot start transaction : %s\n", errBegin)
		WriteError(w, r, errBegin)
		return
	}
	defer tx.Rollback()

	if err := environment.CreateAudit(tx, key, env, c.User); err != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot create audit: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err := environment.DeleteAllVariable(tx, env.ID); err != nil {
		log.Warning("restoreEnvironmentAuditHandler> Cannot delete variables on environments for update: %s\n", err)
		WriteError(w, r, err)
		return
	}

	for varIndex := range auditVars {
		varEnv := &auditVars[varIndex]
		if sdk.NeedPlaceholder(varEnv.Type) {
			value, errDecrypt := secret.Decrypt([]byte(varEnv.Value))
			if errDecrypt != nil {
				log.Warning("restoreEnvironmentAuditHandler> Cannot decrypt variable %s on environment %s: %s\n", varEnv.Name, envName, errDecrypt)
				WriteError(w, r, errDecrypt)
				return
			}
			varEnv.Value = string(value)
		}
		if err := environment.InsertVariable(tx, env.ID, varEnv); err != nil {
			log.Warning("restoreEnvironmentAuditHandler> Cannot insert variables on environments: %s\n", err)
			WriteError(w, r, err)
			return
		}
	}

	lastModified, errDate := project.UpdateProjectDB(db, p.Key, p.Name)
	if errDate != nil {
		log.Warning("restoreEnvironmentAuditHandler> Cannot update project last modified date: %s\n", errDate)
		WriteError(w, r, errDate)
		return
	}
	p.LastModified = lastModified.Unix()

	if err := tx.Commit(); err != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot commit transaction:  %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err := sanity.CheckProjectPipelines(db, p); err != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot check warnings: %s\n", err)
		WriteError(w, r, err)
		return
	}

	var errEnvs error
	p.Environments, errEnvs = environment.LoadEnvironments(db, p.Key, true, c.User)
	if errEnvs != nil {
		log.Warning("restoreEnvironmentAuditHandler: Cannot load environments: %s\n", errEnvs)
		WriteError(w, r, errEnvs)
		return
	}

	WriteJSON(w, r, p, http.StatusOK)
}

func getVariableInEnvironmentHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]
	name := vars["name"]

	v, errVar := environment.GetVariable(db, key, envName, name)
	if errVar != nil {
		log.Warning("getVariableInEnvironmentHandler: Cannot get variable %s for environment %s: %s\n", name, envName, errVar)
		WriteError(w, r, errVar)
		return
	}

	WriteJSON(w, r, v, http.StatusOK)
}

func getVariablesInEnvironmentHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {

	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]

	variables, errVar := environment.GetAllVariable(db, key, envName)
	if errVar != nil {
		log.Warning("getVariablesInEnvironmentHandler: Cannot get variables for environment %s: %s\n", envName, errVar)
		WriteError(w, r, errVar)
		return
	}

	WriteJSON(w, r, variables, http.StatusOK)
}

func deleteVariableFromEnvironmentHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {

	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]
	varName := vars["name"]

	p, errProj := project.LoadProject(db, key, c.User)
	if errProj != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot load project %s :  %s\n", key, errProj)
		WriteError(w, r, errProj)
		return
	}

	env, errEnv := environment.LoadEnvironmentByName(db, key, envName)
	if errEnv != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot load environment %s :  %s\n", envName, errEnv)
		WriteError(w, r, errEnv)
		return
	}

	tx, errBegin := db.Begin()
	if errBegin != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot start transaction:  %s\n", errBegin)
		WriteError(w, r, errBegin)
		return
	}
	defer tx.Rollback()

	if err := environment.CreateAudit(tx, key, env, c.User); err != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot create audit for env %s:  %s\n", envName, err)
		WriteError(w, r, err)
		return
	}

	if err := environment.DeleteVariable(db, env.ID, varName); err != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot delete %s: %s\n", varName, err)
		WriteError(w, r, err)
		return
	}

	lastModified, errDate := project.UpdateProjectDB(db, p.Key, p.Name)
	if errDate != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot update project last modified date: %s\n", errDate)
		WriteError(w, r, errDate)
		return
	}
	p.LastModified = lastModified.Unix()

	if err := tx.Commit(); err != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot commit transaction:  %s\n", err)
		WriteError(w, r, err)
		return
	}

	var errEnvs error
	p.Environments, errEnvs = environment.LoadEnvironments(db, p.Key, true, c.User)
	if errEnvs != nil {
		log.Warning("deleteVariableFromEnvironmentHandler: Cannot load environments: %s\n", errEnvs)
		WriteError(w, r, errEnvs)
		return
	}

	WriteJSON(w, r, p, http.StatusOK)
}

func updateVariableInEnvironmentHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]
	varName := vars["name"]

	p, errProj := project.LoadProject(db, key, c.User)
	if errProj != nil {
		log.Warning("updateVariableInEnvironment: Cannot load %s: %s\n", key, errProj)
		WriteError(w, r, errProj)
		return
	}

	// Get body
	data, errRead := ioutil.ReadAll(r.Body)
	if errRead != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot read body: %s\n", errRead)
		WriteError(w, r, errRead)
		return
	}

	var newVar sdk.Variable
	if err := json.Unmarshal(data, &newVar); err != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot unmarshal body : %s\n", err)
		WriteError(w, r, err)
		return
	}

	env, errEnv := environment.LoadEnvironmentByName(db, key, envName)
	if errEnv != nil {
		log.Warning("updateVariableInEnvironmentHandler: cannot load environment %s: %s\n", envName, errEnv)
		WriteError(w, r, errEnv)
		return
	}

	tx, errBegin := db.Begin()
	if errBegin != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot start transaction:  %s\n", errBegin)
		WriteError(w, r, errBegin)
		return
	}
	defer tx.Rollback()

	if err := environment.CreateAudit(tx, key, env, c.User); err != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot create audit for env %s:  %s\n", envName, err)
		WriteError(w, r, err)
		return
	}

	if err := environment.UpdateVariable(db, env.ID, newVar); err != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot update variable %s for environment %s:  %s\n", varName, envName, err)
		WriteError(w, r, err)
		return
	}

	lastModified, errDate := project.UpdateProjectDB(db, p.Key, p.Name)
	if errDate != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot update project last modified date:  %s\n", errDate)
		WriteError(w, r, errDate)
		return
	}
	p.LastModified = lastModified.Unix()

	if err := tx.Commit(); err != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot commit transaction:  %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err := sanity.CheckProjectPipelines(db, p); err != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot check warnings: %s\n", err)
		WriteError(w, r, err)
		return
	}

	var errEnvs error
	p.Environments, errEnvs = environment.LoadEnvironments(db, p.Key, true, c.User)
	if errEnvs != nil {
		log.Warning("updateVariableInEnvironmentHandler: Cannot load environments: %s\n", errEnvs)
		WriteError(w, r, errEnvs)
		return
	}

	WriteJSON(w, r, p, http.StatusOK)
}

func addVariableInEnvironmentHandler(w http.ResponseWriter, r *http.Request, db *gorp.DbMap, c *context.Context) {
	vars := mux.Vars(r)
	key := vars["key"]
	envName := vars["permEnvironmentName"]
	varName := vars["name"]

	p, errProj := project.LoadProject(db, key, c.User)
	if errProj != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot load %s: %s\n", key, errProj)
		WriteError(w, r, errProj)
		return
	}

	// Get body
	data, errRead := ioutil.ReadAll(r.Body)
	if errRead != nil {
		WriteError(w, r, sdk.ErrWrongRequest)
		return
	}

	var newVar sdk.Variable
	if err := json.Unmarshal(data, &newVar); err != nil {
		WriteError(w, r, sdk.ErrWrongRequest)
		return
	}

	if newVar.Name != varName {
		WriteError(w, r, sdk.ErrWrongRequest)
		return
	}

	env, errEnv := environment.LoadEnvironmentByName(db, key, envName)
	if errEnv != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot load environment %s :  %s\n", envName, errEnv)
		WriteError(w, r, errEnv)
		return
	}

	tx, errBegin := db.Begin()
	if errBegin != nil {
		log.Warning("addVariableInEnvironmentHandler: cannot begin tx: %s\n", errBegin)
		WriteError(w, r, errBegin)
		return
	}
	defer tx.Rollback()

	if err := environment.CreateAudit(tx, key, env, c.User); err != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot create audit for env %s:  %s\n", envName, err)
		WriteError(w, r, err)
		return
	}

	var errInsert error
	switch newVar.Type {
	case sdk.KeyVariable:
		errInsert = environment.AddKeyPairToEnvironment(tx, env.ID, newVar.Name)
	default:
		errInsert = environment.InsertVariable(tx, env.ID, &newVar)
	}
	if errInsert != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot add variable %s in environment %s:  %s\n", varName, envName, errInsert)
		WriteError(w, r, errInsert)
		return
	}

	lastModified, errDate := project.UpdateProjectDB(db, p.Key, p.Name)
	if errDate != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot update project last modified date:  %s\n", errDate)
		WriteError(w, r, errDate)
		return
	}
	p.LastModified = lastModified.Unix()

	if err := tx.Commit(); err != nil {
		log.Warning("addVariableInEnvironmentHandler: cannot commit tx: %s\n", err)
		WriteError(w, r, err)
		return
	}

	if err := sanity.CheckProjectPipelines(db, p); err != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot check warnings: %s\n", err)
		WriteError(w, r, err)
		return
	}

	var errEnvs error
	p.Environments, errEnvs = environment.LoadEnvironments(db, p.Key, true, c.User)
	if errEnvs != nil {
		log.Warning("addVariableInEnvironmentHandler: Cannot load environments: %s\n", errEnvs)
		WriteError(w, r, errEnvs)
		return
	}

	WriteJSON(w, r, p, http.StatusOK)
}
