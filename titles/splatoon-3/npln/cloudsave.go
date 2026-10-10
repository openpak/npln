package npln

// cloudsave — nn.npln.toyohr.v1.CloudSave, the player's server-side save record.
//
// The record is one MapValue (save_data) per player. Facts this follows (docs/provenance.md):
//
//   - The client names its record "tenants/current/saveRecords/current" and gets back a record
//     named under the real tenant.
//   - A player with no record yet uploads the save it built locally with CreateSaveRecord.
//   - WriteSaveRecord carries a PARTIAL save_data: maps merge key by key at every depth, anything
//     else (scalar, array, timestamp, bytes) replaces the stored value, and an empty array is stored
//     as null. The response timestamp is the record's new update_time, which the client sends back
//     as update_time on its next write.
//   - The four-digit name tag ("Identifier") is the server's to assign; ChangeUserName returns it.
//
// Records are kept as one protobuf file per player in NPLN_SAVE_DIR (default: "saves" next to the
// rotation file, i.e. on the state volume). Nothing in a record is logged.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/openpak/npln/proto/common"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	"github.com/openpak/npln/redact"
)

func saveDir() string {
	if d := os.Getenv("NPLN_SAVE_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(envOr("NPLN_ROTATION", "rotation.json")), "saves")
}

type saveStore struct {
	mu  sync.Mutex
	dir string
}

var saves = &saveStore{}

func (s *saveStore) path(uid string) (string, error) {
	// uid comes from a token we signed ("u-" + base32), never from the request; still refuse
	// anything that could leave the directory.
	if uid == "" || strings.ContainsAny(uid, "/\\.") {
		return "", fmt.Errorf("bad user id")
	}
	dir := s.dir
	if dir == "" {
		dir = saveDir()
	}
	return filepath.Join(dir, uid+".pb"), nil
}

// load returns the stored record, or nil when the player has none.
func (s *saveStore) load(uid string) (*toyohrpb.SaveRecord, error) {
	p, err := s.path(uid)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec toyohrpb.SaveRecord
	if err := proto.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *saveStore) store(uid string, rec *toyohrpb.SaveRecord) error {
	p, err := s.path(uid)
	if err != nil {
		return err
	}
	b, err := proto.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func recordName(uid string) string { return Tenant + "/saveRecords/" + uid }

// nameTag is the player's stable four-digit tag, derived from the user id.
func nameTag(uid string) string {
	sum := sha256.Sum256([]byte("splatoon3-name-tag:" + uid))
	return fmt.Sprintf("%04d", binary.BigEndian.Uint32(sum[:4])%10000)
}

func ensureTag(uid string, rec *toyohrpb.SaveRecord) string {
	if rec.SaveData == nil {
		rec.SaveData = &commonpb.MapValue{}
	}
	if rec.SaveData.Fields == nil {
		rec.SaveData.Fields = map[string]*commonpb.Value{}
	}
	if v := rec.SaveData.Fields["Identifier"].GetStringValue(); v != "" {
		return v
	}
	tag := nameTag(uid)
	rec.SaveData.Fields["Identifier"] = sv(tag)
	return tag
}

// normStored stores an empty array as null, at every depth.
func normStored(v *commonpb.Value) *commonpb.Value {
	if av := v.GetArrayValue(); av != nil {
		if len(av.GetValues()) == 0 {
			return &commonpb.Value{ValueType: &commonpb.Value_NullValue{NullValue: structpb.NullValue_NULL_VALUE}}
		}
		out := &commonpb.ArrayValue{}
		for _, x := range av.GetValues() {
			out.Values = append(out.Values, normStored(x))
		}
		return &commonpb.Value{ValueType: &commonpb.Value_ArrayValue{ArrayValue: out}}
	}
	if mv := v.GetMapValue(); mv != nil {
		out := &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}
		for k, x := range mv.GetFields() {
			out.Fields[k] = normStored(x)
		}
		return &commonpb.Value{ValueType: &commonpb.Value_MapValue{MapValue: out}}
	}
	return proto.Clone(v).(*commonpb.Value)
}

// mergeSave applies a partial save_data onto the stored one: maps merge recursively, anything
// else replaces.
func mergeSave(dst, src *commonpb.MapValue) {
	if dst.Fields == nil {
		dst.Fields = map[string]*commonpb.Value{}
	}
	for k, v := range src.GetFields() {
		if sm := v.GetMapValue(); sm != nil {
			if dm := dst.Fields[k].GetMapValue(); dm != nil {
				mergeSave(dm, sm)
				continue
			}
		}
		dst.Fields[k] = normStored(v)
	}
}

type cloudSave struct {
	toyohrpb.UnimplementedCloudSaveServer
}

func saveCaller(ctx context.Context) (string, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return "", status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	return uid, nil
}

// getSaveRecord is raw (rawunary.go): a stored record is one message; no record is NOT_FOUND
// after the headers, never a default record (a default record reads to the client as an existing,
// empty save, and it then never uploads its own).
func getSaveRecord(_ any, ss grpc.ServerStream) error {
	var req toyohrpb.GetSaveRecordRequest
	if err := ss.RecvMsg(&req); err != nil {
		return err
	}
	uid, err := saveCaller(ss.Context())
	if err != nil {
		return err
	}
	saves.mu.Lock()
	rec, err := saves.load(uid)
	saves.mu.Unlock()
	if err != nil {
		log.Printf("[CloudSave] get: %s", redact.Err(err))
		return status.Error(codes.Internal, "save record unavailable")
	}
	if rec == nil {
		log.Printf("[CloudSave] get: no record yet -> NOT_FOUND")
		if err := sendEmptyHeaders(ss); err != nil {
			return err
		}
		return status.Error(codes.NotFound, "save record not found")
	}
	log.Printf("[CloudSave] get: %d keys", len(rec.GetSaveData().GetFields()))
	return ss.SendMsg(rec)
}

func (c *cloudSave) CreateSaveRecord(ctx context.Context, req *toyohrpb.CreateSaveRecordRequest) (*toyohrpb.SaveRecord, error) {
	uid, err := saveCaller(ctx)
	if err != nil {
		return nil, err
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	if old, err := saves.load(uid); err == nil && old != nil {
		log.Printf("[CloudSave] create: a record already exists -> ALREADY_EXISTS")
		return nil, status.Error(codes.AlreadyExists, "save record already exists")
	}
	rec := proto.Clone(req.GetSaveRecord()).(*toyohrpb.SaveRecord)
	if rec == nil {
		rec = &toyohrpb.SaveRecord{}
	}
	data := &commonpb.MapValue{Fields: map[string]*commonpb.Value{}}
	mergeSave(data, rec.GetSaveData())
	rec.SaveData = data
	ensureTag(uid, rec)
	now := timestamppb.New(time.Now().UTC())
	rec.Name, rec.CreateTime, rec.UpdateTime = recordName(uid), now, now
	if err := saves.store(uid, rec); err != nil {
		log.Printf("[CloudSave] create: %s", redact.Err(err))
		return nil, status.Error(codes.Internal, "save record not stored")
	}
	log.Printf("[CloudSave] create: %d keys", len(rec.GetSaveData().GetFields()))
	return rec, nil
}

func (c *cloudSave) ValidateSaveRecord(ctx context.Context, req *toyohrpb.ValidateSaveRecordRequest) (*toyohrpb.ValidateSaveRecordResponse, error) {
	if _, err := saveCaller(ctx); err != nil {
		return nil, err
	}
	return &toyohrpb.ValidateSaveRecordResponse{}, nil
}

func (c *cloudSave) WriteSaveRecord(ctx context.Context, req *toyohrpb.WriteSaveRecordRequest) (*toyohrpb.WriteSaveRecordResponse, error) {
	uid, err := saveCaller(ctx)
	if err != nil {
		return nil, err
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	rec, err := saves.load(uid)
	if err != nil {
		log.Printf("[CloudSave] write: %s", redact.Err(err))
		return nil, status.Error(codes.Internal, "save record unavailable")
	}
	if rec == nil {
		log.Printf("[CloudSave] write without a record -> NOT_FOUND")
		return nil, status.Error(codes.NotFound, "save record not found")
	}
	if req.GetUpdateTime() != nil && rec.GetUpdateTime() != nil && !req.GetUpdateTime().AsTime().Equal(rec.GetUpdateTime().AsTime()) {
		// ponytail: logged, not refused. The real service's answer to a stale update_time is
		// unmeasured; refusing it blind could lose a player's progress.
		log.Printf("[CloudSave] write: update_time is not the stored one (applied anyway)")
	}
	if rec.SaveData == nil {
		rec.SaveData = &commonpb.MapValue{}
	}
	mergeSave(rec.SaveData, req.GetSaveRecord().GetSaveData())
	ensureTag(uid, rec)
	now := time.Now().UTC()
	if prev := rec.GetUpdateTime().AsTime(); !now.After(prev) {
		now = prev.Add(time.Millisecond) // strictly increasing: it is the client's next token
	}
	rec.UpdateTime = timestamppb.New(now)
	if err := saves.store(uid, rec); err != nil {
		log.Printf("[CloudSave] write: %s", redact.Err(err))
		return nil, status.Error(codes.Internal, "save record not stored")
	}
	log.Printf("[CloudSave] write %s: %d keys changed", lastSeg(req.GetSaveEventType()), len(req.GetSaveRecord().GetSaveData().GetFields()))
	return &toyohrpb.WriteSaveRecordResponse{Timestamp: rec.UpdateTime}, nil
}

func (c *cloudSave) ChangeUserName(ctx context.Context, req *toyohrpb.ChangeUserNameRequest) (*toyohrpb.ChangeUserNameResponse, error) {
	uid, err := saveCaller(ctx)
	if err != nil {
		return nil, err
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	rec, err := saves.load(uid)
	if err != nil || rec == nil {
		return nil, status.Error(codes.NotFound, "save record not found")
	}
	tag := ensureTag(uid, rec)
	rec.SaveData.Fields["UserName"] = sv(req.GetUserName())
	now := timestamppb.New(time.Now().UTC())
	rec.UpdateTime = now
	if err := saves.store(uid, rec); err != nil {
		log.Printf("[CloudSave] rename: %s", redact.Err(err))
		return nil, status.Error(codes.Internal, "save record not stored")
	}
	return &toyohrpb.ChangeUserNameResponse{Identifier: tag, Timestamp1: now, Timestamp2: now}, nil
}

func (c *cloudSave) DeleteSaveRecord(ctx context.Context, req *toyohrpb.DeleteSaveRecordRequest) (*toyohrpb.DeleteSaveRecordResponse, error) {
	uid, err := saveCaller(ctx)
	if err != nil {
		return nil, err
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	p, err := saves.path(uid)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return nil, status.Error(codes.Internal, "save record not deleted")
	}
	return &toyohrpb.DeleteSaveRecordResponse{}, nil
}
