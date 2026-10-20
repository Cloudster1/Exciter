package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

// The suite demo (#270): Armature's issues, Stator's pages and Exciter's requirements, each product's own route.
const (
	demoPath  = "/installation/suite-demo"
	demoLinks = demoPath + "/links"
	// demoWait bounds how long the seed waits for one product to build its part; demoPoll is how often it looks.
	demoWait = 10 * time.Minute
	demoPoll = 3 * time.Second
)

// demoState is what each product answers on GET /installation/suite-demo.
type demoState struct {
	Demo *struct {
		State   string `json:"state"`
		Message string `json:"message"`
		Links   int    `json:"links"`
		Skipped int    `json:"skipped"`
	} `json:"demo"`
}

// notBuilt is a product that has no suite demo yet, which answers its route as it answers any it does not know.
func notBuilt(err error) bool {
	var r *refusal
	return errors.As(err, &r) && slices.Contains([]int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented}, r.status)
}

// addDemo adds the suite demo to each product as its administrator, Exciter's last, since it links to the others'.
// Exciter's administrator links with alice's Armature token and a Stator token of Stator's administrator.
func addDemo(ctx context.Context, exciter, armature, stator *session, armatureToken string) error {
	statorToken, err := stator.freshToken(ctx, "the Stator administrator's")
	if err != nil {
		return err
	}
	for path, token := range map[string]string{"/armature/account/token": armatureToken, "/stator/account/token": statorToken} {
		if err := exciter.send(ctx, http.MethodPut, path, map[string]any{"token": token}, nil); err != nil {
			return fmt.Errorf("store Exciter's administrator's token at %s: %w", path, err)
		}
	}
	for _, p := range []struct {
		name string
		api  *session
	}{{"Armature", armature}, {"Stator", stator}} {
		err := p.api.send(ctx, http.MethodPost, demoPath, nil, nil)
		switch {
		case notBuilt(err):
			slog.Info("this version has no suite demo yet, so Exciter's links to it wait", "product", p.name)
			continue
		case err != nil && !isConflict(err):
			return fmt.Errorf("add %s's suite demo: %w", p.name, err)
		}
		if _, err := waitForDemo(ctx, p.name, p.api); err != nil {
			return err
		}
	}
	err = exciter.send(ctx, http.MethodPost, demoPath, nil, nil)
	if isConflict(err) {
		err = exciter.send(ctx, http.MethodPost, demoLinks, nil, nil)
	}
	if err != nil && !isConflict(err) {
		return fmt.Errorf("add Exciter's suite demo: %w", err)
	}
	got, err := waitForDemo(ctx, "Exciter", exciter)
	if err == nil && got.Demo != nil {
		slog.Info("the suite demo is linked", "links", got.Demo.Links, "waiting", got.Demo.Skipped, "note", got.Demo.Message)
	}
	return err
}

// waitForDemo reads a product's demo until it is done.
func waitForDemo(ctx context.Context, name string, api *session) (demoState, error) {
	for deadline := time.Now().Add(demoWait); time.Now().Before(deadline); time.Sleep(demoPoll) {
		var got demoState
		if err := api.send(ctx, http.MethodGet, demoPath, nil, &got); err != nil {
			return got, fmt.Errorf("read %s's suite demo: %w", name, err)
		}
		switch {
		case got.Demo == nil:
			return got, fmt.Errorf("%s says nobody added its suite demo", name)
		case got.Demo.State == "done":
			slog.Info("suite demo ready", "product", name)
			return got, nil
		case got.Demo.State == "failed":
			return got, fmt.Errorf("%s's suite demo stopped: %s", name, got.Demo.Message)
		}
	}
	return demoState{}, fmt.Errorf("%s's suite demo is still being built; look at it in its settings", name)
}
