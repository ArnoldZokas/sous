package sous

import (
	"database/sql"
	"fmt"
	"log"

	// triggers the loading of sqlite3 as a database driver
	"github.com/docker/distribution/reference"
	_ "github.com/mattn/go-sqlite3"
	"github.com/opentable/sous/util/docker_registry"
	"github.com/samsalisbury/semv"
)

type (
	// NameCache is a database for looking up SourceVersions based on
	// Docker image names and vice versa.
	NameCache struct {
		registryClient docker_registry.Client
		db             *sql.DB
	}

	imageName string

	// NotModifiedErr is returned when an HTTP server returns Not Modified in
	// response to a conditional request
	NotModifiedErr struct{}

	// NoImageNameFound is returned when we cannot find an image name for a given
	// SourceVersion
	NoImageNameFound struct {
		SourceVersion
	}

	// NoSourceVersionFound is returned when we cannot find a SourceVersion for a
	// given image name
	NoSourceVersionFound struct {
		imageName
	}

	// ImageMapper interface describes the component responsible for mapping
	// source versions to names
	ImageMapper interface {
		// GetCanonicalName returns the canonical name for an image given any known
		// name
		GetCanonicalName(in string) (string, error)

		// Insert puts a given SourceVersion/image name pair into the name cache
		Insert(sv SourceVersion, in, etag string) error

		// GetImageName returns the docker image name for a given source version
		GetImageName(sv SourceVersion) (string, error)

		// GetSourceVersion returns the source version for a given image name
		GetSourceVersion(in string) (SourceVersion, error)
	}
)

// InMemory configures SQLite to use an in-memory database
// The dummy file allows multiple goroutines see the same in-memory DB
const InMemory = "file:dummy.db?mode=memory&cache=shared"

// InMemoryConnection builds a connection string based on a base name
// This is mostly useful for testing, so that we can have separate cache DBs per test
func InMemoryConnection(base string) string {
	return "file:" + base + "?mode=memory&cache=shared"
}

func (e NoImageNameFound) Error() string {
	return fmt.Sprintf("No image name for %v", e.SourceVersion)
}

func (e NoSourceVersionFound) Error() string {
	return fmt.Sprintf("No source version for %v", e.imageName)
}

func (e NotModifiedErr) Error() string {
	return "Not modified"
}

// NewNameCache builds a new name cache
func NewNameCache(cl docker_registry.Client, dbCfg ...string) *NameCache {
	db, err := getDatabase(dbCfg...)
	if err != nil {
		log.Fatal("Error building name cache DB: ", err)
	}

	return &NameCache{cl, db}
}

// GetSourceVersion looks up the source version for a given image name
func (nc *NameCache) GetSourceVersion(in string) (SourceVersion, error) {
	var sv SourceVersion

	Log.Debug.Print(in)

	etag, repo, offset, version, _, err := nc.dbQueryOnName(in)
	if nif, ok := err.(NoSourceVersionFound); ok {
		Log.Debug.Print(nif)
	} else if err != nil {
		Log.Debug.Print("Err: ", err)
		return SourceVersion{}, err
	} else {
		Log.Debug.Printf("Found: %v %v %v", repo, offset, version)

		sv, err = makeSourceVersion(repo, offset, version)
		if err != nil {
			return sv, err
		}
	}

	md, err := nc.registryClient.GetImageMetadata(in, etag)
	Log.Debug.Printf("%+ v %v", md, err)
	if _, ok := err.(NotModifiedErr); ok {
		return sv, nil
	}
	if err != nil {
		return sv, err
	}

	newSV, err := SourceVersionFromLabels(md.Labels)
	if err != nil {
		return sv, err
	}

	err = nc.dbInsert(newSV, md.CanonicalName, md.Etag)
	if err != nil {
		return sv, err
	}

	Log.Debug.Printf("cn: %v all: %v", md.CanonicalName, md.AllNames)
	err = nc.dbAddNames(md.CanonicalName, md.AllNames)

	return newSV, err
}

func (nc *NameCache) harvest(sl SourceLocation) error {
	repos, err := nc.dbQueryOnSL(sl)
	if err != nil {
		return err
	}
	for _, r := range repos {
		ref, err := reference.ParseNamed(r)
		if err != nil {
			return fmt.Errorf("%v for %v", err, r)
		}
		ts, err := nc.registryClient.AllTags(r)
		if err == nil {
			for _, t := range ts {
				in, err := reference.WithTag(ref, t)
				if err == nil {
					nc.GetSourceVersion(in.String()) //pull it into the cache...
				}
			}
		}
	}
	return nil
}

// GetImageName returns the docker image name for a given source version
func (nc *NameCache) GetImageName(sv SourceVersion) (string, error) {
	Log.Debug.Printf("Getting image name for %+v", sv)
	cn, _, err := nc.dbQueryOnSV(sv)
	if _, ok := err.(NoImageNameFound); ok {
		err = nc.harvest(sv.CanonicalName())
		if err != nil {
			return "", err
		}

		cn, _, err = nc.dbQueryOnSV(sv)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return cn, nil
}

// GetCanonicalName returns the canonical name for an image given any known name
func (nc *NameCache) GetCanonicalName(in string) (string, error) {
	_, _, _, _, cn, err := nc.dbQueryOnName(in)
	Log.Debug.Print(cn)
	return cn, err
}

// Insert puts a given SourceVersion/image name pair into the name cache
func (nc *NameCache) Insert(sv SourceVersion, in, etag string) error {
	return nc.dbInsert(sv, in, etag)
}

func union(left, right []string) []string {
	set := make(map[string]struct{})
	for _, s := range left {
		set[s] = struct{}{}
	}

	for _, s := range right {
		set[s] = struct{}{}
	}

	res := make([]string, 0, len(set))

	for k := range set {
		res = append(res, k)
	}

	return res
}

func getDatabase(cfg ...string) (*sql.DB, error) {
	driver := "sqlite3"
	conn := InMemory
	if len(cfg) >= 1 {
		driver = cfg[0]
	}

	if len(cfg) >= 2 {
		conn = cfg[1]
	}

	db, err := sql.Open(driver, conn) //only call once
	if err != nil {
		return nil, err
	}

	if err := sqlExec(db, "pragma foreign_keys = ON;"); err != nil {
		return nil, err
	}

	if err := sqlExec(db, "create table if not exists docker_repo_name("+
		"repo_name_id integer primary key autoincrement"+
		", name text not null"+
		", constraint upsertable unique (name) on conflict replace"+
		");"); err != nil {
		return nil, err
	}

	if err := sqlExec(db, "create table if not exists docker_search_location("+
		"location_id integer primary key autoincrement, "+
		"repo text not null, "+
		"offset text not null, "+
		"constraint upsertable unique (repo, offset) on conflict replace"+
		");"); err != nil {
		return nil, err
	}

	if err := sqlExec(db, "create table if not exists repo_through_location("+
		"repo_name_id references docker_repo_name "+
		"   on delete cascade on update cascade not null, "+
		"location_id references docker_search_location "+
		"   on delete cascade on update cascade not null "+
		",  primary key (repo_name_id, location_id) on conflict replace"+
		");"); err != nil {
		return nil, err
	}

	if err := sqlExec(db, "create table if not exists docker_search_metadata("+
		"metadata_id integer primary key autoincrement, "+
		"location_id references docker_search_location "+
		"   on delete cascade on update cascade not null, "+
		"etag text not null, "+
		"canonicalName text not null, "+
		"version text not null, "+
		"constraint upsertable unique (location_id, version) on conflict replace"+
		");"); err != nil {
		return nil, err
	}

	if err := sqlExec(db, "create table if not exists docker_search_name("+
		"name_id integer primary key autoincrement, "+
		"metadata_id references docker_search_metadata "+
		"   on delete cascade on update cascade not null, "+
		"name text not null unique on conflict replace"+
		");"); err != nil {
		return nil, err
	}

	return db, err
}

func sqlExec(db *sql.DB, sql string) error {
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("Error: %s in SQL: %s", err, sql)
	}
	return nil
}

func (nc *NameCache) dbInsert(sv SourceVersion, in, etag string) error {
	ref, err := reference.ParseNamed(in)
	if err != nil {
		return fmt.Errorf("%v for %v", err, in)
	}

	Log.Debug.Print(ref.Name())
	nr, err := nc.db.Exec("insert into docker_repo_name "+
		"(name) values ($1);", ref.Name())
	nid, err := nr.LastInsertId()
	if err != nil {
		return err
	}

	res, err := nc.db.Exec("insert into docker_search_location "+
		"(repo, offset) values ($1, $2);",
		string(sv.RepoURL), string(sv.RepoOffset))

	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}

	_, err = nc.db.Exec("insert into repo_through_location "+
		"(repo_name_id, location_id) values ($1, $2)", nid, id)
	if err != nil {
		return err
	}

	Log.Debug.Printf("%v %v %v %v", id, etag, in, sv.Version)
	res, err = nc.db.Exec("insert into docker_search_metadata "+
		"(location_id, etag, canonicalName, version) values ($1, $2, $3, $4);",
		id, etag, in, sv.Version.Format(semv.MMPPre))

	if err != nil {
		return err
	}

	id, err = res.LastInsertId()
	if err != nil {
		return err
	}

	res, err = nc.db.Exec("insert into docker_search_name "+
		"(metadata_id, name) values ($1, $2)", id, in)

	return err
}

func (nc *NameCache) dbAddNames(cn string, ins []string) error {
	var id int
	row := nc.db.QueryRow("select metadata_id from docker_search_metadata "+
		"where canonicalName = $1", cn)
	err := row.Scan(&id)
	if err != nil {
		return err
	}
	add, err := nc.db.Prepare("insert into docker_search_name " +
		"(metadata_id, name) values ($1, $2)")
	if err != nil {
		return err
	}

	for _, n := range ins {
		_, err := add.Exec(id, n)
		if err != nil {
			return err
		}
	}

	return nil
}

func (nc *NameCache) dbQueryOnName(in string) (etag, repo, offset, version, cname string, err error) {
	row := nc.db.QueryRow("select "+
		"docker_search_metadata.etag, "+
		"docker_search_location.repo, "+
		"docker_search_location.offset, "+
		"docker_search_metadata.version, "+
		"docker_search_metadata.canonicalName "+
		"from "+
		"docker_search_name natural join docker_search_metadata "+
		"natural join docker_search_location "+
		"where docker_search_name.name = $1", in)
	err = row.Scan(&etag, &repo, &offset, &version, &cname)
	if err == sql.ErrNoRows {
		err = NoSourceVersionFound{imageName(in)}
	}
	return
}

func (nc *NameCache) dbQueryOnSL(sl SourceLocation) (rs []string, err error) {
	rows, err := nc.db.Query("select docker_repo_name.name "+
		"from "+
		"docker_repo_name natural join repo_through_location "+
		"  natural join docker_search_location "+
		"where "+
		"docker_search_location.repo = $1 and "+
		"docker_search_location.offset = $2",
		string(sl.RepoURL), string(sl.RepoOffset))

	if err == sql.ErrNoRows {
		return []string{}, err
	}
	if err != nil {
		return []string{}, err
	}

	for rows.Next() {
		var r string
		rows.Scan(&r)
		rs = append(rs, r)
	}
	err = rows.Err()
	if len(rs) == 0 {
		err = fmt.Errorf("no repos found for %+v", sl)
	}
	return
}

func (nc *NameCache) dbQueryOnSV(sv SourceVersion) (cn string, ins []string, err error) {
	ins = make([]string, 0)
	rows, err := nc.db.Query("select docker_search_metadata.canonicalName, "+
		"docker_search_name.name "+
		"from "+
		"docker_search_name natural join docker_search_metadata "+
		"natural join docker_search_location "+
		"where "+
		"docker_search_location.repo = $1 and "+
		"docker_search_location.offset = $2 and "+
		"docker_search_metadata.version = $3",
		string(sv.RepoURL), string(sv.RepoOffset), sv.Version.String())

	if err == sql.ErrNoRows {
		err = NoImageNameFound{sv}
		return
	}
	if err != nil {
		return
	}

	for rows.Next() {
		var in string
		rows.Scan(&cn, &in)
		ins = append(ins, in)
	}
	err = rows.Err()
	if len(ins) == 0 {
		err = NoImageNameFound{sv}
	}

	return
}

func makeSourceVersion(repo, offset, version string) (SourceVersion, error) {
	v, err := semv.Parse(version)
	if err != nil {
		return SourceVersion{}, err
	}

	return SourceVersion{
		RepoURL(repo), v, RepoOffset(offset),
	}, nil
}
