// Package smmadmin serves a small password-protected web page for
// moderating Super Mario Maker: list/delete courses, boost a course's star
// count, mark PIDs as "verified" (Official Makers), and manually register
// an already-valid course file an admin has and wants placed directly (not
// a level generator - this project has no way to produce Nintendo's real
// course binary format).
package smmadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Protarium-Network/super-mario-maker-nex-server/globals"
	"github.com/Protarium-Network/super-mario-maker-nex-server/smmdatabase"
	"github.com/PretendoNetwork/nex-go/v2/types"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var adminPassword string
var minioClient *minio.Client
var s3Bucket string

// Matches the real export filename convention, e.g. "course-1023052.bin",
// to auto-fill the DataID field when the admin doesn't type it in by hand.
var courseFilenameDataIDPattern = regexp.MustCompile(`course-(\d+)`)

func Start() {
	adminPassword = strings.TrimSpace(os.Getenv("PN_SMM_ADMIN_PASSWORD"))
	if adminPassword == "" {
		globals.Logger.Warning("[SMM Admin] PN_SMM_ADMIN_PASSWORD not set - admin panel disabled")
		return
	}

	s3Endpoint := os.Getenv("PN_S3_ENDPOINT")
	s3Bucket = os.Getenv("PN_S3_BUCKET")
	if s3Bucket == "" {
		s3Bucket = "smm-datastore"
	}

	if s3Endpoint != "" {
		client, err := minio.New(s3Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(os.Getenv("PN_S3_ACCESS_KEY"), os.Getenv("PN_S3_SECRET_KEY"), ""),
			Secure: true,
		})
		if err != nil {
			globals.Logger.Errorf("[SMM Admin] Failed to create MinIO client: %s", err.Error())
		} else {
			minioClient = client
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", requireAuth(handleIndex))
	mux.HandleFunc("/delete", requireAuth(handleDelete))
	mux.HandleFunc("/stars", requireAuth(handleStars))
	mux.HandleFunc("/plays", requireAuth(handlePlays))
	mux.HandleFunc("/verify", requireAuth(handleVerify))
	mux.HandleFunc("/import", requireAuth(handleImport))
	mux.HandleFunc("/metadata", requireAuth(handleMetadata))

	listenAddress := os.Getenv("PN_SMM_ADMIN_LISTEN")
	if listenAddress == "" {
		listenAddress = "127.0.0.1:8097"
	}

	globals.Logger.Successf("[SMM Admin] Listening on %s", listenAddress)
	if err := http.ListenAndServe(listenAddress, mux); err != nil {
		globals.Logger.Error(err.Error())
	}
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if !ok || password != adminPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="smm-admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

var pageTemplate = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="utf-8">
<title>Super Mario Maker - Admin</title>
<style>
	body { font-family: -apple-system, sans-serif; max-width: 1100px; margin: 2rem auto; padding: 0 1rem; background: #14161a; color: #e6e6e6; }
	h1 { font-size: 1.3rem; }
	h2 { font-size: 1rem; margin-top: 2rem; border-bottom: 1px solid #333; padding-bottom: 0.3rem; }
	table { border-collapse: collapse; width: 100%; margin-bottom: 1.5rem; }
	th, td { border: 1px solid #333; padding: 0.4rem 0.6rem; text-align: left; font-size: 0.85rem; }
	th { background: #1f2228; }
	tr:nth-child(even) { background: #191b20; }
	form.inline { display: inline-flex; gap: 0.3rem; align-items: center; }
	button { cursor: pointer; background: #2a7; color: white; border: none; padding: 0.25rem 0.6rem; border-radius: 3px; }
	button.danger { background: #d33; }
	input[type=number], input[type=text] { width: 5rem; padding: 0.2rem; background: #14161a; color: #eee; border: 1px solid #444; border-radius: 3px; }
	.box { background: #1f2228; padding: 1rem; border-radius: 6px; margin-bottom: 1.5rem; }
	.box label { display: block; margin-top: 0.5rem; font-size: 0.8rem; color: #aaa; }
	.box input[type=file], .box input[type=text], .box input[type=number] { width: 100%; padding: 0.3rem; box-sizing: border-box; background: #14161a; color: #eee; border: 1px solid #444; border-radius: 3px; margin-top: 0.2rem; }
	.hint { color: #888; font-size: 0.75rem; }
	.searchbar { margin-bottom: 1rem; }
	.searchbar input { width: 20rem; padding: 0.4rem; background: #1f2228; color: #eee; border: 1px solid #444; border-radius: 3px; }
	a { color: #6cf; }
	.checkrow { display: flex; align-items: center; gap: 0.4rem; margin-top: 0.6rem; }
	.checkrow input { width: auto; }
</style>
</head>
<body>
<h1>🍄 Super Mario Maker — Admin</h1>

<div class="searchbar">
<form method="GET" action="/">
	<input type="text" name="q" placeholder="Filtrer par DataID ou PID créateur" value="{{.Search}}">
	<button type="submit">Filtrer</button>
</form>
</div>

<h2>Importer un niveau existant</h2>
<div class="box">
<p class="hint">Enregistre un fichier de niveau déjà valide (ex: ton propre niveau exporté) directement en base + S3, avec ses vraies métadonnées (JSON). Ne génère aucun contenu - il faut fournir des fichiers réels.</p>
<form method="POST" action="/import" enctype="multipart/form-data">
	<label>PID du créateur (propriétaire) - utilisé seulement si absent du JSON</label>
	<input type="text" name="owner_pid">
	<label>Nom du niveau - utilisé seulement si absent du JSON</label>
	<input type="text" name="name">
	<label>DataID d'origine (optionnel) - laisse vide pour en générer un nouveau. Pour un niveau réellement exporté (ex: fichier "course-1023052.bin"), remets le vrai ID (1023052) : le fichier .bin embarque son propre DataID dans un checksum interne, et le jeu affiche "données corrompues" si l'ID sous lequel il est servi ne correspond plus à celui codé dans le fichier.</label>
	<input type="text" name="explicit_data_id" placeholder="ex: 1023052">
	<label>Fichier de niveau (.bin)</label>
	<input type="file" name="course_file" required>
	<label>Métadonnées (.json) - optionnel mais recommandé (stars, plays, clears, record du monde, etc.)</label>
	<input type="file" name="metadata_file">
	<button type="submit" style="margin-top:0.8rem;">Importer</button>
</form>
</div>

<h2>Créateurs vérifiés (section MAKERS)</h2>
<div class="box">
<form class="inline" method="POST" action="/verify">
	<label style="display:inline;">PID :</label>
	<input type="text" name="pid" required>
	<button type="submit" name="action" value="add">Vérifier</button>
</form>
<table>
<tr><th>PID</th><th></th></tr>
{{range .VerifiedMakers}}
<tr>
	<td>{{.}}</td>
	<td>
		<form class="inline" method="POST" action="/verify">
			<input type="hidden" name="pid" value="{{.}}">
			<button class="danger" type="submit" name="action" value="remove">Retirer</button>
		</form>
	</td>
</tr>
{{end}}
</table>
</div>

<h2>Niveaux ({{len .Courses}})</h2>
<table>
<tr><th>DataID</th><th>Créateur (PID)</th><th>Nom</th><th>Taille</th><th>⭐</th><th>▶️</th><th>Supprimé</th><th>Actions</th></tr>
{{range .Courses}}
<tr>
	<td>{{.DataID}}</td>
	<td>{{.Owner}}</td>
	<td>{{.Name}}</td>
	<td>{{.Size}}</td>
	<td>{{.Stars}}</td>
	<td>{{.Plays}}</td>
	<td>{{if .Deleted}}oui{{else}}—{{end}}</td>
	<td>
		<form class="inline" method="POST" action="/stars">
			<input type="hidden" name="data_id" value="{{.DataID}}">
			<input type="number" name="amount" value="10" min="0" style="width:4rem;">
			<button type="submit" name="direction" value="add">⭐ Ajouter</button>
			<button type="submit" name="direction" value="remove">⭐ Enlever</button>
		</form>
		<form class="inline" method="POST" action="/plays">
			<input type="hidden" name="data_id" value="{{.DataID}}">
			<input type="number" name="amount" value="10" min="0" style="width:4rem;">
			<button type="submit" name="direction" value="add">▶️ Ajouter</button>
			<button type="submit" name="direction" value="remove">▶️ Enlever</button>
		</form>
		<a href="/metadata?data_id={{.DataID}}">Métadonnées</a>
		<form class="inline" method="POST" action="/delete" onsubmit="return confirm('Supprimer ce niveau ?');">
			<input type="hidden" name="data_id" value="{{.DataID}}">
			<button class="danger" type="submit">Suppr</button>
		</form>
	</td>
</tr>
{{end}}
</table>

</body>
</html>`))

var metadataTemplate = template.Must(template.New("metadata").Parse(`<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="utf-8">
<title>Métadonnées — DataID {{.DataID}}</title>
<style>
	body { font-family: -apple-system, sans-serif; max-width: 600px; margin: 2rem auto; padding: 0 1rem; background: #14161a; color: #e6e6e6; }
	h1 { font-size: 1.2rem; }
	.box { background: #1f2228; padding: 1rem; border-radius: 6px; }
	.box label { display: block; margin-top: 0.7rem; font-size: 0.8rem; color: #aaa; }
	.box input[type=text], .box input[type=number] { width: 100%; padding: 0.3rem; box-sizing: border-box; background: #14161a; color: #eee; border: 1px solid #444; border-radius: 3px; margin-top: 0.2rem; }
	.checkrow { display: flex; align-items: center; gap: 0.4rem; margin-top: 0.7rem; }
	.checkrow input { width: auto; }
	button { cursor: pointer; background: #2a7; color: white; border: none; padding: 0.4rem 0.8rem; border-radius: 3px; margin-top: 1rem; }
	a { color: #6cf; }
</style>
</head>
<body>
<p><a href="/">&larr; Retour</a></p>
<h1>Métadonnées — DataID {{.DataID}}</h1>
<div class="box">
<form method="POST" action="/metadata">
	<input type="hidden" name="data_id" value="{{.DataID}}">
	<div class="checkrow"><input type="checkbox" name="is_event_course" {{if .IsEventCourse}}checked{{end}}> <label style="margin:0;">Niveau événement</label></div>
	<div class="checkrow"><input type="checkbox" name="is_official_maker_course" {{if .IsOfficialMakerCourse}}checked{{end}}> <label style="margin:0;">Niveau de créateur officiel</label></div>
	<label>Clears (réussites)</label>
	<input type="number" name="clears" value="{{.Clears}}">
	<label>Total attempts (essais totaux)</label>
	<input type="number" name="total_attempts" value="{{.TotalAttempts}}">
	<label>Failures (échecs)</label>
	<input type="number" name="failures" value="{{.Failures}}">
	<label>Commentaires Miiverse</label>
	<input type="number" name="miiverse_comments" value="{{.MiiverseComments}}">
	<label>PID du record du monde</label>
	<input type="text" name="world_record_pid" value="{{.WorldRecordPID}}">
	<label>Temps du record du monde (ms)</label>
	<input type="number" name="world_record_time_ms" value="{{.WorldRecordTimeMs}}">
	<button type="submit">Enregistrer</button>
</form>
</div>
</body>
</html>`))

type pageData struct {
	Search         string
	Courses        []smmdatabase.CourseRow
	VerifiedMakers []uint64
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	courses, err := smmdatabase.ListCourses(search, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	verified, err := smmdatabase.ListVerifiedMakers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, pageData{Search: search, Courses: courses, VerifiedMakers: verified}); err != nil {
		globals.Logger.Error(err.Error())
	}
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dataID, err := strconv.ParseUint(r.FormValue("data_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid data_id", http.StatusBadRequest)
		return
	}

	if err := smmdatabase.AdminDeleteCourse(dataID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	globals.Logger.Infof("[SMM Admin] deleted course data_id=%d", dataID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleStars(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dataID, err1 := strconv.ParseUint(r.FormValue("data_id"), 10, 64)
	amount, err2 := strconv.ParseInt(r.FormValue("amount"), 10, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid form values", http.StatusBadRequest)
		return
	}
	if r.FormValue("direction") == "remove" {
		amount = -amount
	}

	if err := smmdatabase.AdminAddStars(dataID, amount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	globals.Logger.Infof("[SMM Admin] added %d stars to data_id=%d", amount, dataID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handlePlays(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dataID, err1 := strconv.ParseUint(r.FormValue("data_id"), 10, 64)
	amount, err2 := strconv.ParseInt(r.FormValue("amount"), 10, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid form values", http.StatusBadRequest)
		return
	}
	if r.FormValue("direction") == "remove" {
		amount = -amount
	}

	if err := smmdatabase.AdminAddPlays(dataID, amount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	globals.Logger.Infof("[SMM Admin] added %d plays to data_id=%d", amount, dataID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		dataID, err := strconv.ParseUint(r.FormValue("data_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid data_id", http.StatusBadRequest)
			return
		}

		clears, _ := strconv.ParseInt(r.FormValue("clears"), 10, 64)
		totalAttempts, _ := strconv.ParseInt(r.FormValue("total_attempts"), 10, 64)
		failures, _ := strconv.ParseInt(r.FormValue("failures"), 10, 64)
		comments, _ := strconv.ParseInt(r.FormValue("miiverse_comments"), 10, 64)
		wrPID, _ := strconv.ParseUint(r.FormValue("world_record_pid"), 10, 32)
		wrTimeMs, _ := strconv.ParseInt(r.FormValue("world_record_time_ms"), 10, 64)

		meta := smmdatabase.CourseMetadata{
			DataID:                dataID,
			IsEventCourse:         r.FormValue("is_event_course") == "on",
			IsOfficialMakerCourse: r.FormValue("is_official_maker_course") == "on",
			Clears:                clears,
			TotalAttempts:         totalAttempts,
			Failures:              failures,
			MiiverseComments:      comments,
			WorldRecordPID:        uint32(wrPID),
			WorldRecordTimeMs:     wrTimeMs,
		}

		if err := smmdatabase.UpsertCourseMetadata(meta); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		globals.Logger.Infof("[SMM Admin] updated metadata for data_id=%d", dataID)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	dataID, err := strconv.ParseUint(r.URL.Query().Get("data_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid data_id", http.StatusBadRequest)
		return
	}

	meta, err := smmdatabase.GetCourseMetadata(dataID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := metadataTemplate.Execute(w, meta); err != nil {
		globals.Logger.Error(err.Error())
	}
}

func handleVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pid, err := strconv.ParseUint(r.FormValue("pid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	verified := r.FormValue("action") != "remove"
	if err := smmdatabase.AdminSetVerifiedMaker(pid, verified); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	globals.Logger.Infof("[SMM Admin] set verified maker pid=%d verified=%t", pid, verified)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// importMetadataJSON mirrors the real per-course metadata format (field
// names confirmed from a real course's own extracted metadata file) - "miis"
// is ignored (Mii data comes from the account system, not course metadata).
//
// Unknown1/Unknown2 were previously believed unused, but Pretendo's own
// archival tool (github.com/PretendoNetwork/smm1-course-archive,
// archive.py) confirms these - along with plays/clears/total_attempts/
// failures/miiverse_comments - all come from the SAME real mechanism: 7
// DataStore rating slots (0-6) attached to the object, not a side table.
// An admin-imported course that never has those 7 slots populated is
// missing something the real client apparently expects when loading a
// course, which is what AdminSetCourseRatings (called from handleImport)
// now fixes.
type importMetadataJSON struct {
	IsEventCourse         bool `json:"is_event_course"`
	IsOfficialMakerCourse bool `json:"is_official_maker_course"`
	WorldRecord           struct {
		BestTimePID      uint32 `json:"best_time_pid"`
		FirstCompletePID uint32 `json:"first_complete_pid"`
		TimeMilliseconds int64  `json:"time_milliseconds"`
		CreatedTime      uint64 `json:"created_time"`
		UpdatedTime      uint64 `json:"updated_time"`
	} `json:"world_record"`
	Stars            int64  `json:"stars"`
	CourseName       string `json:"course_name"`
	CreatorPID       uint32 `json:"creator_pid"`
	UserPlays        int64  `json:"user_plays"`
	Unknown1         int64  `json:"unknown1"`
	Clears           int64  `json:"clears"`
	TotalAttempts    int64  `json:"total_attempts"`
	Failures         int64  `json:"failures"`
	Unknown2         int64  `json:"unknown2"`
	MiiverseComments int64  `json:"miiverse_comments"`
}

func handleImport(w http.ResponseWriter, r *http.Request) {
	if minioClient == nil {
		http.Error(w, "S3 not configured", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Metadata JSON is optional but, when provided, is the source of truth
	// for owner/name/stars/plays/etc - the manual form fields are only a
	// fallback for when no JSON is supplied.
	var meta importMetadataJSON
	haveMeta := false
	if metaFile, _, err := r.FormFile("metadata_file"); err == nil {
		defer metaFile.Close()
		if err := json.NewDecoder(metaFile).Decode(&meta); err != nil {
			http.Error(w, "invalid metadata_file: "+err.Error(), http.StatusBadRequest)
			return
		}
		haveMeta = true
	}

	ownerPID := meta.CreatorPID
	if ownerPID == 0 {
		parsed, err := strconv.ParseUint(strings.TrimSpace(r.FormValue("owner_pid")), 10, 32)
		if err != nil {
			http.Error(w, "owner_pid required (either in metadata_file as creator_pid, or the form field)", http.StatusBadRequest)
			return
		}
		ownerPID = uint32(parsed)
	}

	name := meta.CourseName
	if name == "" {
		name = strings.TrimSpace(r.FormValue("name"))
	}
	if name == "" {
		http.Error(w, "name required (either in metadata_file as course_name, or the form field)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("course_file")
	if err != nil {
		http.Error(w, "missing course_file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read fully into memory: the raw bytes are needed both to compute the
	// real meta_binary (segment lengths + CRC32s - see
	// smmdatabase.BuildCourseMetaBinary) and to upload to S3, and the
	// meta_binary MUST be derived from these exact bytes or the client's own
	// CRC check on the course object rejects it as corrupted.
	rawCourseData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read course_file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	metaBinary, err := smmdatabase.BuildCourseMetaBinary(rawCourseData)
	if err != nil {
		http.Error(w, "course_file does not look like a valid course object: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Real course .bin files embed the DataID they were originally uploaded
	// under (part of the share-code checksum) - importing under a freshly
	// auto-generated DataID instead makes the client's own consistency
	// check fail and show the level as "corrupted" data. Accept an explicit
	// override, falling back to auto-detecting it from filenames following
	// the real export convention "course-<dataid>.bin" / "course-<dataid>-metadata.json".
	var explicitDataID uint64
	if raw := strings.TrimSpace(r.FormValue("explicit_data_id")); raw != "" {
		explicitDataID, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "explicit_data_id must be a number", http.StatusBadRequest)
			return
		}
	} else if match := courseFilenameDataIDPattern.FindStringSubmatch(header.Filename); match != nil {
		explicitDataID, _ = strconv.ParseUint(match[1], 10, 64)
	}

	dataID, err := smmdatabase.AdminImportCourse(ownerPID, name, uint32(len(rawCourseData)), explicitDataID, metaBinary)
	if err != nil {
		if errors.Is(err, smmdatabase.ErrCourseAlreadyExists) {
			http.Error(w, fmt.Sprintf("Niveau déjà présent sur le serveur (DataID %d déjà utilisé) - vérifie le champ explicit_data_id.", explicitDataID), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	key := fmt.Sprintf("smm/%d.bin", dataID)
	if _, err := minioClient.PutObject(context.Background(), s3Bucket, key, bytes.NewReader(rawCourseData), int64(len(rawCourseData)), minio.PutObjectOptions{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// A real client only ever gets a smm/<id>.jpg through a separate
	// PrepareAttachFile round-trip after uploading its own screenshot - an
	// admin import never goes through that, so without this the Course
	// World browse list has no thumbnail to show and falls back to a
	// generic placeholder for every import (see ExtractCourseThumbnailJPEG's
	// docs). Extract the course's own real thumbnail and upload it under
	// the same key a real client would have used.
	if thumbJPEG, err := smmdatabase.ExtractCourseThumbnailJPEG(rawCourseData); err != nil {
		globals.Logger.Errorf("[SMM Admin] failed to extract thumbnail for data_id=%d: %s", dataID, err.Error())
	} else {
		jpgKey := fmt.Sprintf("smm/%d.jpg", dataID)
		if _, err := minioClient.PutObject(context.Background(), s3Bucket, jpgKey, bytes.NewReader(thumbJPEG), int64(len(thumbJPEG)), minio.PutObjectOptions{ContentType: "image/jpeg"}); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to upload thumbnail for data_id=%d: %s", dataID, err.Error())
		} else if err := smmdatabase.AdminRegisterCourseAttachFile(ownerPID, dataID, uint32(len(thumbJPEG))); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to register attach-file object for data_id=%d: %s", dataID, err.Error())
		}
	}

	if haveMeta {
		if err := smmdatabase.AdminSetStars(dataID, meta.Stars); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to set imported stars: %s", err.Error())
		}
		if err := smmdatabase.AdminAddPlays(dataID, meta.UserPlays); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to set imported plays: %s", err.Error())
		}
		if err := smmdatabase.AdminSetCourseRatings(dataID, meta.UserPlays, meta.Unknown1, meta.Clears, meta.TotalAttempts, meta.Failures, meta.Unknown2, meta.MiiverseComments); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to set imported course rating slots: %s", err.Error())
		}
		if meta.WorldRecord.BestTimePID != 0 {
			// Nintendo's NEX DateTime is a packed uint64 (see
			// nex-go/v2/types/datetime.go), not a Unix timestamp.
			created := types.DateTime(meta.WorldRecord.CreatedTime).Standard()
			updated := types.DateTime(meta.WorldRecord.UpdatedTime).Standard()
			if err := smmdatabase.AdminSetCourseRecord(dataID, meta.WorldRecord.FirstCompletePID, meta.WorldRecord.BestTimePID, int32(meta.WorldRecord.TimeMilliseconds), created, updated); err != nil {
				globals.Logger.Errorf("[SMM Admin] failed to set imported world record: %s", err.Error())
			}
		}
		courseMeta := smmdatabase.CourseMetadata{
			DataID:                dataID,
			IsEventCourse:         meta.IsEventCourse,
			IsOfficialMakerCourse: meta.IsOfficialMakerCourse,
			Clears:                meta.Clears,
			TotalAttempts:         meta.TotalAttempts,
			Failures:              meta.Failures,
			MiiverseComments:      meta.MiiverseComments,
			WorldRecordPID:        meta.WorldRecord.BestTimePID,
			WorldRecordTimeMs:     meta.WorldRecord.TimeMilliseconds,
		}
		if err := smmdatabase.UpsertCourseMetadata(courseMeta); err != nil {
			globals.Logger.Errorf("[SMM Admin] failed to set imported metadata: %s", err.Error())
		}
	}

	globals.Logger.Infof("[SMM Admin] imported course data_id=%d owner=%d name=%q size=%d metadata=%t", dataID, ownerPID, name, header.Size, haveMeta)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
