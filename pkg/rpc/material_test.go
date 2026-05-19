package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"apisrv/pkg/db"

	. "github.com/smartystreets/goconvey/convey"
)

// makeMaterial creates a material via material.add as the given admin and
// returns the resulting Material. Order is omitted so MAX+1-on-Add is exercised.
func makeMaterial(t *testing.T, hs *httptest.Server, adminKey, title, mtype, url string) Material {
	t.Helper()
	r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Add, map[string]any{
		"title":       title,
		"type":        mtype,
		"url":         url,
		"description": "",
	})
	if r.Error != nil {
		t.Fatalf("material.add %s: code=%d %s", title, r.Error.Code, r.Error.Message)
	}
	var m Material
	if err := json.Unmarshal(r.Result, &m); err != nil {
		t.Fatalf("unmarshal material: %v", err)
	}
	return m
}

func unmarshalCandidateMaterial(t *testing.T, raw json.RawMessage) *CandidateMaterial {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cm CandidateMaterial
	if err := json.Unmarshal(raw, &cm); err != nil {
		t.Fatalf("unmarshal CandidateMaterial: %v", err)
	}
	return &cm
}

// =============================================================================
// MaterialService — Add / Update / Order auto-increment
// =============================================================================

func TestDB_MaterialService_AddUpdate(t *testing.T) {
	Convey("MaterialService Add/Update", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.material")

		Convey("Add without order → first material gets order=1", func() {
			m := makeMaterial(t, hs, adminKey, "Go book", "book", "https://go.dev")
			So(m.Order, ShouldEqual, 1)
			So(m.MaxScore, ShouldEqual, 10)
			So(m.Title, ShouldEqual, "Go book")
		})

		Convey("Add second without order → MAX+1", func() {
			_ = makeMaterial(t, hs, adminKey, "First", "book", "https://example.com/1")
			second := makeMaterial(t, hs, adminKey, "Second", "article", "https://example.com/2")
			So(second.Order, ShouldEqual, 2)
		})

		Convey("Add with explicit order honoured", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Add, map[string]any{
				"title": "Explicit", "type": "video", "url": "https://example.com/v",
				"description": "", "order": 42,
			})
			So(r.Error, ShouldBeNil)
			var m Material
			So(json.Unmarshal(r.Result, &m), ShouldBeNil)
			So(m.Order, ShouldEqual, 42)
		})

		Convey("Add: duplicate title → 400", func() {
			_ = makeMaterial(t, hs, adminKey, "Dup", "book", "https://example.com/x")
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Add, map[string]any{
				"title": "Dup", "type": "book", "url": "https://example.com/y",
				"description": "",
			})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("Add: invalid type → 400", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Add, map[string]any{
				"title": "Bad", "type": "podcast", "url": "https://example.com/p",
				"description": "",
			})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("Update without order preserves existing order", func() {
			m := makeMaterial(t, hs, adminKey, "Orig", "book", "https://example.com/o")
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Update, m.ID, map[string]any{
				"title": "Renamed", "type": "book", "url": "https://example.com/o",
				"description": "updated",
			})
			So(r.Error, ShouldBeNil)
			var updated Material
			So(json.Unmarshal(r.Result, &updated), ShouldBeNil)
			So(updated.Title, ShouldEqual, "Renamed")
			So(updated.Order, ShouldEqual, m.Order)
		})

		Convey("Delete is soft (statusId=3) and frees title", func() {
			m := makeMaterial(t, hs, adminKey, "Old", "book", "https://example.com/o")
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Delete, m.ID)
			So(r.Error, ShouldBeNil)

			// title is now reusable.
			again := makeMaterial(t, hs, adminKey, "Old", "article", "https://example.com/o2")
			So(again.ID, ShouldNotEqual, m.ID)
		})

		Convey("Add: createdAt and updatedAt are stamped (regression: use_zero wrote 0001-01-01)", func() {
			zero := time.Time{}
			before := time.Now().Add(-time.Second)
			m := makeMaterial(t, hs, adminKey, "Stamped", "article", "https://example.com/s")

			created, err := time.Parse(time.RFC3339, m.CreatedAt)
			So(err, ShouldBeNil)
			So(created.Equal(zero), ShouldBeFalse) // regression: was 0001-01-01
			So(created.After(before), ShouldBeTrue)

			updated, err := time.Parse(time.RFC3339, m.UpdatedAt)
			So(err, ShouldBeNil)
			So(updated.Equal(zero), ShouldBeFalse) // regression: was 0001-01-01
			So(updated.After(before), ShouldBeTrue)

			// Independent read-back: ensure the persisted row carries the
			// same stamps the add response advertised.
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.GetByID, m.ID)
			So(r.Error, ShouldBeNil)
			var fromDB Material
			So(json.Unmarshal(r.Result, &fromDB), ShouldBeNil)
			So(fromDB.CreatedAt, ShouldEqual, m.CreatedAt)
			So(fromDB.UpdatedAt, ShouldEqual, m.UpdatedAt)
		})

		Convey("Update: bumps updatedAt, persists in DB, leaves createdAt alone", func() {
			zero := time.Time{}
			before := time.Now().Add(-time.Second)
			m := makeMaterial(t, hs, adminKey, "Bumped", "book", "https://example.com/b")
			prevUpdated, err := time.Parse(time.RFC3339, m.UpdatedAt)
			So(err, ShouldBeNil)
			So(prevUpdated.Equal(zero), ShouldBeFalse) // regression: was 0001-01-01

			created, err := time.Parse(time.RFC3339, m.CreatedAt)
			So(err, ShouldBeNil)

			// RFC3339 truncates to seconds; sleep so the new stamp is strictly later.
			time.Sleep(1100 * time.Millisecond)

			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Update, m.ID, map[string]any{
				"title": "Bumped", "type": "book", "url": "https://example.com/b",
				"description": "new desc",
			})
			So(r.Error, ShouldBeNil)
			var updated Material
			So(json.Unmarshal(r.Result, &updated), ShouldBeNil)

			next, err := time.Parse(time.RFC3339, updated.UpdatedAt)
			So(err, ShouldBeNil)
			So(next.Equal(zero), ShouldBeFalse) // regression: was 0001-01-01
			So(next.After(prevUpdated), ShouldBeTrue)
			So(next.After(before), ShouldBeTrue)

			// createdAt must not move on Update.
			createdAfter, err := time.Parse(time.RFC3339, updated.CreatedAt)
			So(err, ShouldBeNil)
			So(createdAfter.Equal(created), ShouldBeTrue)

			// Independent read-back: the DB row carries the same timestamps the
			// update response advertised. Catches a bug where the response is
			// stamped in-memory but the persisted row diverges.
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.GetByID, m.ID)
			So(r.Error, ShouldBeNil)
			var fromDB Material
			So(json.Unmarshal(r.Result, &fromDB), ShouldBeNil)
			So(fromDB.UpdatedAt, ShouldEqual, updated.UpdatedAt)
			So(fromDB.CreatedAt, ShouldEqual, updated.CreatedAt)
		})

		Convey("Update twice: second call bumps updatedAt past the first update", func() {
			m := makeMaterial(t, hs, adminKey, "Twice", "book", "https://example.com/2x")

			time.Sleep(1100 * time.Millisecond)
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Update, m.ID, map[string]any{
				"title": "Twice", "type": "book", "url": "https://example.com/2x",
				"description": "first",
			})
			So(r.Error, ShouldBeNil)
			var first Material
			So(json.Unmarshal(r.Result, &first), ShouldBeNil)
			firstUpdated, err := time.Parse(time.RFC3339, first.UpdatedAt)
			So(err, ShouldBeNil)

			time.Sleep(1100 * time.Millisecond)
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Update, m.ID, map[string]any{
				"title": "Twice", "type": "book", "url": "https://example.com/2x",
				"description": "second",
			})
			So(r.Error, ShouldBeNil)
			var second Material
			So(json.Unmarshal(r.Result, &second), ShouldBeNil)
			secondUpdated, err := time.Parse(time.RFC3339, second.UpdatedAt)
			So(err, ShouldBeNil)

			So(secondUpdated.After(firstUpdated), ShouldBeTrue)
		})
	})
}

// =============================================================================
// MaterialService.Reorder — full permutation under advisory lock
// =============================================================================

func TestDB_MaterialService_Reorder(t *testing.T) {
	Convey("MaterialService.Reorder", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.matreorder")

		// Seed three materials with auto-assigned orders 1,2,3.
		m1 := makeMaterial(t, hs, adminKey, "Alpha", "book", "https://example.com/a")
		m2 := makeMaterial(t, hs, adminKey, "Bravo", "article", "https://example.com/b")
		m3 := makeMaterial(t, hs, adminKey, "Charlie", "video", "https://example.com/c")
		So(m1.Order, ShouldEqual, 1)
		So(m2.Order, ShouldEqual, 2)
		So(m3.Order, ShouldEqual, 3)

		Convey("full permutation succeeds and returns the new order", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m3.ID, m1.ID, m2.ID})
			So(r.Error, ShouldBeNil)
			var out []Material
			So(json.Unmarshal(r.Result, &out), ShouldBeNil)
			So(out, ShouldHaveLength, 3)
			So(out[0].ID, ShouldEqual, m3.ID)
			So(out[0].Order, ShouldEqual, 1)
			So(out[1].ID, ShouldEqual, m1.ID)
			So(out[1].Order, ShouldEqual, 2)
			So(out[2].ID, ShouldEqual, m2.ID)
			So(out[2].Order, ShouldEqual, 3)
		})

		Convey("post-reorder catalog has no non-positive orders", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m3.ID, m2.ID, m1.ID})
			So(r.Error, ShouldBeNil)
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Get, nil)
			So(r.Error, ShouldBeNil)
			var list []Material
			So(json.Unmarshal(r.Result, &list), ShouldBeNil)
			for _, m := range list {
				So(m.Order, ShouldBeGreaterThanOrEqualTo, 1)
			}
		})

		Convey("rejects mismatched length", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m1.ID, m2.ID})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("rejects unknown id", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m1.ID, m2.ID, 999999})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("rejects duplicate id", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m1.ID, m1.ID, m2.ID})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("rejects soft-deleted id (not in active set)", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Delete, m2.ID)
			So(r.Error, ShouldBeNil)
			// 2 active left, but caller still passes the dead one.
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Reorder,
				[]int{m1.ID, m2.ID, m3.ID})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("admin-only: anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSMaterial, RPC.MaterialService.Reorder,
				[]int{m1.ID, m2.ID, m3.ID})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})
	})
}

// =============================================================================
// MaterialService.SetRead — central candidate-side invariant
// =============================================================================

func TestDB_MaterialService_SetRead(t *testing.T) {
	Convey("MaterialService.SetRead", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.read")

		// Seed one stage so registerCandidate succeeds.
		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		_, candKey := registerCandidate(t, ctx, hs, dbo, "ivan.read")

		mat := makeMaterial(t, hs, adminKey, "Tutorial", "video", "https://example.com/t")

		Convey("first setRead(true) creates row with readAt", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(cm, ShouldNotBeNil)
			So(cm.ReadAt, ShouldNotBeNil)
			So(cm.MaterialID, ShouldEqual, mat.ID)
			So(cm.Score, ShouldBeNil)
		})

		Convey("repeated setRead(true) keeps the original readAt", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)
			first := unmarshalCandidateMaterial(t, r.Result)
			So(first.ReadAt, ShouldNotBeNil)

			// Sleep ~1s so timestamps would visibly differ if the code re-stamped.
			time.Sleep(1100 * time.Millisecond)

			r = rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)
			second := unmarshalCandidateMaterial(t, r.Result)
			So(second.ReadAt, ShouldNotBeNil)
			So(*second.ReadAt, ShouldEqual, *first.ReadAt)
		})

		Convey("setRead(false) clears readAt when not yet scored", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, false)
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(cm, ShouldNotBeNil)
			So(cm.ReadAt, ShouldBeNil)
		})

		Convey("setRead(false) without any record → 404", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, false)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusNotFound)
		})

		Convey("setRead(false) after admin scored → 400 ErrMaterialAlreadyScored", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)
			cand := lookupCandidate(t, ctx, dbo, "ivan.read")
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score, cand.ID, mat.ID, 8, nil)
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, false)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)

			// readAt is preserved, score still set.
			cm := loadCandidateMaterial(t, ctx, dbo, cand.ID, mat.ID)
			So(cm.ReadAt, ShouldNotBeNil)
			So(cm.Score, ShouldNotBeNil)
		})

		Convey("setRead on unknown material → 404", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, 99999, true)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusNotFound)
		})
	})
}

// =============================================================================
// MaterialService.Score / Unscore — admin path + COALESCE-on-notes
// =============================================================================

func TestDB_MaterialService_ScoreUnscore(t *testing.T) {
	Convey("MaterialService.Score / Unscore", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.score")

		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		_, _ = registerCandidate(t, ctx, hs, dbo, "ivan.score")
		cand := lookupCandidate(t, ctx, dbo, "ivan.score")

		mat := makeMaterial(t, hs, adminKey, "Compilers", "book", "https://example.com/c")

		Convey("Score happy path: creates row even if candidate never marked read", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 8, "well done")
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(cm.Score, ShouldNotBeNil)
			So(*cm.Score, ShouldEqual, 8)
			So(cm.ScoredAt, ShouldNotBeNil)
			So(cm.Notes, ShouldNotBeNil)
			So(*cm.Notes, ShouldEqual, "well done")
			So(cm.ReadAt, ShouldBeNil)
		})

		Convey("Score COALESCE: re-score with nil notes preserves prior comment", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 7, "first try")
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(*cm.Notes, ShouldEqual, "first try")

			// Re-score without notes — old comment must survive.
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 9, nil)
			So(r.Error, ShouldBeNil)
			cm = unmarshalCandidateMaterial(t, r.Result)
			So(*cm.Score, ShouldEqual, 9)
			So(cm.Notes, ShouldNotBeNil)
			So(*cm.Notes, ShouldEqual, "first try")
		})

		Convey("Score with new notes overwrites prior comment", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 7, "old")
			So(r.Error, ShouldBeNil)
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 8, "new")
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(*cm.Notes, ShouldEqual, "new")
		})

		Convey("Score with whitespace-only notes is normalised to nil (no overwrite)", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 6, "kept")
			So(r.Error, ShouldBeNil)
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 7, "   ")
			So(r.Error, ShouldBeNil)
			cm := unmarshalCandidateMaterial(t, r.Result)
			So(cm.Notes, ShouldNotBeNil)
			So(*cm.Notes, ShouldEqual, "kept")
		})

		Convey("Score out of range → 400", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 999, nil)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)
		})

		Convey("Score on unknown material → 404", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, 99999, 8, nil)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusNotFound)
		})

		Convey("Unscore clears all four fields and is idempotent", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 8, "n")
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Unscore, cand.ID, mat.ID)
			So(r.Error, ShouldBeNil)
			var cleared bool
			So(json.Unmarshal(r.Result, &cleared), ShouldBeNil)
			So(cleared, ShouldBeTrue)

			cm := loadCandidateMaterial(t, ctx, dbo, cand.ID, mat.ID)
			So(cm.Score, ShouldBeNil)
			So(cm.ScoredAt, ShouldBeNil)
			So(cm.ScoredBy, ShouldBeNil)
			So(cm.Notes, ShouldBeNil)

			// repeat Unscore → false (already cleared).
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Unscore, cand.ID, mat.ID)
			So(r.Error, ShouldBeNil)
			So(json.Unmarshal(r.Result, &cleared), ShouldBeNil)
			So(cleared, ShouldBeFalse)
		})

		Convey("After Unscore the candidate can setRead(false) again", func() {
			// candidate marks read.
			candKey := loginCandidate(t, hs, "ivan.score")
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)

			// admin scores → setRead(false) blocked.
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Score, cand.ID, mat.ID, 8, nil)
			So(r.Error, ShouldBeNil)
			r = rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, false)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusBadRequest)

			// admin unscores → setRead(false) now works.
			r = rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.Unscore, cand.ID, mat.ID)
			So(r.Error, ShouldBeNil)
			r = rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, false)
			So(r.Error, ShouldBeNil)
		})
	})
}

// =============================================================================
// MaterialService — auth tiers
// =============================================================================

func TestDB_MaterialService_Authorization(t *testing.T) {
	Convey("MaterialService auth tiers", t, func() {
		hs, dbo := newHTTPHarness(t)
		ctx := t.Context()
		adminKey := seedAdmin(t, ctx, dbo, "admin.authmat")

		stageResp := rpcCall(t, hs, adminKey, NSStage, RPC.StageService.Add, map[string]any{
			"alias": "s1", "order": 1, "title": "t", "shortTitle": "s", "maxScore": 10,
		})
		So(stageResp.Error, ShouldBeNil)

		_, candKey := registerCandidate(t, ctx, hs, dbo, "petr.auth")
		cand := lookupCandidate(t, ctx, dbo, "petr.auth")

		mat := makeMaterial(t, hs, adminKey, "Networks", "book", "https://example.com/n")

		Convey("getProgress is open (no auth needed)", func() {
			r := rpcCall(t, hs, "", NSMaterial, RPC.MaterialService.GetProgress)
			So(r.Error, ShouldBeNil)
			var mp MaterialsProgress
			So(json.Unmarshal(r.Result, &mp), ShouldBeNil)
			So(mp.Materials, ShouldHaveLength, 1)
			So(mp.Candidates, ShouldHaveLength, 1)
			// no candidateMaterials yet.
			So(mp.Progress, ShouldHaveLength, 0)
		})

		Convey("getProgress reflects new candidateMaterials after setRead", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldBeNil)

			r = rpcCall(t, hs, "", NSMaterial, RPC.MaterialService.GetProgress)
			So(r.Error, ShouldBeNil)
			var mp MaterialsProgress
			So(json.Unmarshal(r.Result, &mp), ShouldBeNil)
			So(mp.Progress, ShouldHaveLength, 1)
			So(mp.Progress[0].CandidateID, ShouldEqual, cand.ID)
			So(mp.Progress[0].MaterialID, ShouldEqual, mat.ID)
		})

		Convey("setRead requires candidate context: admin → 403", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusForbidden)
		})

		Convey("setRead requires auth header: anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSMaterial, RPC.MaterialService.SetRead, mat.ID, true)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("score is admin-only: candidate → 401", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 8, nil)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("score is admin-only: anonymous → 401", func() {
			r := rpcCall(t, hs, "", NSMaterial, RPC.MaterialService.Score,
				cand.ID, mat.ID, 8, nil)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})

		Convey("getMyProgress requires candidate context: admin → 403", func() {
			r := rpcCall(t, hs, adminKey, NSMaterial, RPC.MaterialService.GetMyProgress)
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusForbidden)
		})

		Convey("getMyProgress as candidate returns row per material", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.GetMyProgress)
			So(r.Error, ShouldBeNil)
			var rows []MyMaterialProgress
			So(json.Unmarshal(r.Result, &rows), ShouldBeNil)
			So(rows, ShouldHaveLength, 1)
			So(rows[0].Material.ID, ShouldEqual, mat.ID)
			So(rows[0].Progress, ShouldBeNil)
		})

		Convey("add is admin-only: candidate → 401", func() {
			r := rpcCall(t, hs, candKey, NSMaterial, RPC.MaterialService.Add, map[string]any{
				"title": "x", "type": "book", "url": "https://example.com/x",
				"description": "",
			})
			So(r.Error, ShouldNotBeNil)
			So(r.Error.Code, ShouldEqual, http.StatusUnauthorized)
		})
	})
}

// =============================================================================
// helpers
// =============================================================================

// lookupCandidate fetches a candidate by login from the DB. Used because
// auth.register returns only the authKey, so the test needs another path
// to get the candidate row.
func lookupCandidate(t *testing.T, ctx context.Context, dbo db.DB, login string) *db.Candidate {
	t.Helper()
	c, err := db.NewApprenticeRepo(dbo).EnabledCandidateByLogin(ctx, login)
	if err != nil || c == nil {
		t.Fatalf("lookup candidate %s: %v", login, err)
	}
	return c
}

// loadCandidateMaterial reads the candidateMaterials row directly from the DB,
// for assertions that aren't visible through the RPC return value.
func loadCandidateMaterial(t *testing.T, ctx context.Context, dbo db.DB, candidateID, materialID int) *db.CandidateMaterial {
	t.Helper()
	cm, err := db.NewApprenticeRepo(dbo).OneCandidateMaterial(ctx, &db.CandidateMaterialSearch{
		CandidateID: &candidateID,
		MaterialID:  &materialID,
	}, db.WithColumns(db.TableColumns))
	if err != nil {
		t.Fatalf("load candidateMaterial: %v", err)
	}
	if cm == nil {
		t.Fatalf("load candidateMaterial: row not found for (%d, %d)", candidateID, materialID)
	}
	return cm
}

// loginCandidate logs an existing candidate in (re-using the seeded password
// from registerCandidate) and returns the fresh authKey.
func loginCandidate(t *testing.T, hs *httptest.Server, login string) string {
	t.Helper()
	r := rpcCall(t, hs, "", NSAuth, RPC.AuthService.Login, login, "passw0rd!", UserTypeUser)
	if r.Error != nil {
		t.Fatalf("login %s: %d %s", login, r.Error.Code, r.Error.Message)
	}
	var key string
	if err := json.Unmarshal(r.Result, &key); err != nil {
		t.Fatalf("unmarshal authKey: %v", err)
	}
	return key
}
