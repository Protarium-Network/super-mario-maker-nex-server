package nex_smm

// Plain-HTTPS "pickup" service for 100 Mario Challenge's "New Game" flow.
// Unlike every other SMM browsing feature (which goes over the NEX/PRUDP
// DataStore protocol handled elsewhere in this package), the client fetches
// its 100 Mario course pool via a REST call to the real Nintendo domain
// wup-ama.app.nintendo.net/api/v1/pickup/{easy,normal} - this was never
// implemented anywhere in this project (it's not part of the NEX layer at
// all), so the domain went unresolved/unrouted and the client fell back to
// its generic "this service will be available soon" message with no error
// code, since it's client-rendered rather than a system network error.
//
// The exact response schema isn't publicly documented anywhere; the per-item
// shape `{"url":"%s", "course_id":"%lld"}` (course_id as a quoted string,
// per the literal format string) was recovered by inspecting the game's own
// compiled code - a mechanical string-format observation, not any
// reproduction of Nintendo's actual proprietary curated pickup content
// (which this project was never in possession of to begin with; both
// endpoints below serve real player-uploaded courses instead).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/EcrazerDev/super-mario-maker/globals"
	"github.com/EcrazerDev/super-mario-maker/smmdatabase"
)

type pickupEntry struct {
	URL      string `json:"url"`
	CourseID string `json:"course_id"`
}

const (
	easyPickupPoolSize        = 8
	standardPickupPoolSize    = 16
	superExpertPickupPoolSize = 6
)

func pickupPoolSize(path string) (int, bool) {
	difficulty := strings.TrimPrefix(path, "/api/v1/pickup/")
	switch difficulty {
	case "easy":
		return easyPickupPoolSize, true
	case "normal", "expert":
		return standardPickupPoolSize, true
	case "super_expert", "super-expert", "superexpert":
		return superExpertPickupPoolSize, true
	default:
		return 0, false
	}
}

func handlePickup(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	poolSize, ok := pickupPoolSize(request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}

	// Difficulty isn't tracked per-course yet (see smmdatabase/course_metadata.go),
	// so both tiers currently draw from the same pool of real uploaded
	// courses - a working, non-empty pickup list rather than blocking on a
	// full difficulty-rating system.
	courses, nexError := smmdatabase.GetRandomCoursesWithLimit(poolSize)
	if nexError != nil {
		http.Error(writer, "internal error", http.StatusInternalServerError)
		return
	}

	entries := make([]pickupEntry, 0, len(courses))
	for _, course := range courses {
		dataID := uint64(course.MetaInfo.DataID)
		key := fmt.Sprintf("smm/%d.bin", dataID)

		url, err := presigner.GetObject(s3Bucket(), key, time.Hour)
		if err != nil {
			continue
		}

		entries = append(entries, pickupEntry{
			URL:      url.String(),
			CourseID: fmt.Sprintf("%d", dataID),
		})
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(entries)
}

// StartPickupServer listens for the plain-HTTPS 100 Mario pickup requests,
// separate from the UDP NEX servers above. Deployments should terminate
// legacy-compatible TLS in a reverse proxy and route the console's original
// pickup hostname to this endpoint.
func StartPickupServer() {
	listen := os.Getenv("PN_SMM_PICKUP_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8098"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pickup/", handlePickup)

	go func() {
		if err := http.ListenAndServe(listen, mux); err != nil {
			globals.Logger.Critical("[SMM Pickup] " + err.Error())
		}
	}()

	globals.Logger.Successf("[SMM Pickup] Listening on %s", listen)
}
