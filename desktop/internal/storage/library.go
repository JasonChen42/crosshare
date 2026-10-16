package storage

import (
	"database/sql"
	"time"
)

// LibraryItem is a physical desktop asset, independent of the phone that sent it.
// LocalPath is deliberately never exposed by the API.
type LibraryItem struct {
	Hash      string    `json:"hash"`
	Name      string    `json:"name"`
	Date      time.Time `json:"date"`
	Size      int64     `json:"size"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Favorite  bool      `json:"favorite"`
	LocalPath string    `json:"local_path,omitempty"`
}
type Transfer struct {
	ID        int64  `json:"id"`
	Hash      string `json:"hash"`
	Device    string `json:"device_id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	Updated   string `json:"updated"`
	Name      string `json:"name"`
	AlbumID   int64  `json:"album_id"`
	AlbumName string `json:"album_name"`
}
type Album struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (d *DB) migrateLibrary() error {
	_, err := d.db.Exec(`
 CREATE INDEX IF NOT EXISTS idx_library_date ON synced_files(date_taken DESC,id DESC);
 CREATE INDEX IF NOT EXISTS idx_library_hash_id ON synced_files(content_hash,id);
 CREATE TABLE IF NOT EXISTS library_flags(hash TEXT PRIMARY KEY, favorite INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS delivery_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT, hash TEXT NOT NULL, device_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', error TEXT NOT NULL DEFAULT '', updated TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, album_id INTEGER NOT NULL DEFAULT 0, UNIQUE(hash,device_id,album_id));
 CREATE TABLE IF NOT EXISTS albums(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE);
 CREATE TABLE IF NOT EXISTS album_items(album_id INTEGER NOT NULL, hash TEXT NOT NULL, PRIMARY KEY(album_id,hash));
 `)
	if err != nil {
		return err
	}
	var columns int
	if err = d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('delivery_tasks') WHERE name='album_id'").Scan(&columns); err != nil {
		return err
	}
	if columns == 0 {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		_, err = tx.Exec(`CREATE TABLE delivery_tasks_v2(id INTEGER PRIMARY KEY AUTOINCREMENT,hash TEXT NOT NULL,device_id TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',error TEXT NOT NULL DEFAULT '',updated TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,album_id INTEGER NOT NULL DEFAULT 0,UNIQUE(hash,device_id,album_id));
        INSERT INTO delivery_tasks_v2(id,hash,device_id,status,error,updated) SELECT id,hash,device_id,status,error,updated FROM delivery_tasks;
        DROP TABLE delivery_tasks; ALTER TABLE delivery_tasks_v2 RENAME TO delivery_tasks;`)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	return nil
}
func (d *DB) Library(limit, offset int, query string, album int64, favorite bool) ([]LibraryItem, error) {
	rows, err := d.db.Query(`SELECT f.content_hash,f.original_name,f.date_taken,f.file_size,f.media_type,f.device_id,COALESCE(g.favorite,0),f.local_path
 FROM synced_files f LEFT JOIN library_flags g ON g.hash=f.content_hash
 WHERE f.id=(SELECT MIN(f2.id) FROM synced_files f2 WHERE f2.content_hash=f.content_hash)
 AND (?='' OR instr(lower(f.original_name),lower(?))>0)
 AND (?=0 OR EXISTS(SELECT 1 FROM album_items a WHERE a.hash=f.content_hash AND a.album_id=?))
 AND (?=0 OR g.favorite=1)
 ORDER BY f.date_taken DESC,f.id DESC LIMIT ? OFFSET ?`, query, query, album, album, favorite, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]LibraryItem, 0)
	for rows.Next() {
		var f LibraryItem
		if err = rows.Scan(&f.Hash, &f.Name, &f.Date, &f.Size, &f.Kind, &f.Source, &f.Favorite, &f.LocalPath); err != nil {
			return nil, err
		}
		items = append(items, f)
	}
	return items, rows.Err()
}
func (d *DB) LibraryAsset(hash string) (*LibraryItem, error) {
	f := new(LibraryItem)
	err := d.db.QueryRow(`SELECT content_hash,original_name,date_taken,file_size,media_type,device_id,local_path FROM synced_files WHERE content_hash=? ORDER BY id LIMIT 1`, hash).Scan(&f.Hash, &f.Name, &f.Date, &f.Size, &f.Kind, &f.Source, &f.LocalPath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}
func (d *DB) SetFavorite(hash string, value bool) error {
	_, err := d.db.Exec(`INSERT INTO library_flags(hash,favorite) VALUES(?,?) ON CONFLICT(hash) DO UPDATE SET favorite=excluded.favorite`, hash, value)
	return err
}
func (d *DB) QueueDelivery(hash, device string) error {
	_, err := d.db.Exec(`INSERT INTO delivery_tasks(hash,device_id) VALUES(?,?) ON CONFLICT(hash,device_id,album_id) DO UPDATE SET status='pending',error='',updated=CURRENT_TIMESTAMP`, hash, device)
	return err
}
func (d *DB) Deliveries(device string) ([]Transfer, error) {
	return d.DeliveriesPage(device, 48, 0, false)
}
func (d *DB) DeliveriesPage(device string, limit, offset int, pending bool) ([]Transfer, error) {
	rows, err := d.db.Query(`SELECT t.id,t.hash,t.device_id,t.status,t.error,t.updated,COALESCE((SELECT original_name FROM synced_files WHERE content_hash=t.hash LIMIT 1),''),t.album_id,COALESCE((SELECT name FROM albums WHERE id=t.album_id),'') FROM delivery_tasks t WHERE (?='' OR t.device_id=?) AND (?=0 OR t.status='pending') ORDER BY (t.status='pending') DESC, t.id ASC LIMIT ? OFFSET ?`, device, device, pending, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Transfer, 0)
	for rows.Next() {
		var t Transfer
		if err = rows.Scan(&t.ID, &t.Hash, &t.Device, &t.Status, &t.Error, &t.Updated, &t.Name, &t.AlbumID, &t.AlbumName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (d *DB) AckDelivery(id int64, device, problem string) (bool, error) {
	status := "done"
	if problem != "" {
		status = "pending"
	}
	res, err := d.db.Exec(`UPDATE delivery_tasks SET status=?,error=?,updated=CURRENT_TIMESTAMP WHERE id=? AND device_id=?`, status, problem, id, device)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
func (d *DB) Albums() ([]Album, error) {
	rows, err := d.db.Query(`SELECT a.id,a.name,COUNT(i.hash) FROM albums a LEFT JOIN album_items i ON i.album_id=a.id GROUP BY a.id ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Album, 0)
	for rows.Next() {
		var a Album
		if err = rows.Scan(&a.ID, &a.Name, &a.Count); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (d *DB) CreateAlbum(name string) (int64, error) {
	_, err := d.db.Exec(`INSERT INTO albums(name) VALUES(?) ON CONFLICT(name) DO NOTHING`, name)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.db.QueryRow(`SELECT id FROM albums WHERE name=?`, name).Scan(&id)
	return id, err
}
func (d *DB) AddToAlbum(id int64, hash string) error {
	res, err := d.db.Exec(`INSERT OR IGNORE INTO album_items(album_id,hash) SELECT id,? FROM albums WHERE id=?`, hash, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		err = d.db.QueryRow(`SELECT 1 FROM albums WHERE id=?`, id).Scan(&exists)
	}
	return err
}

func (d *DB) LibraryCount(query string, album int64, favorite bool) (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM synced_files f LEFT JOIN library_flags g ON g.hash=f.content_hash
 WHERE f.id=(SELECT MIN(f2.id) FROM synced_files f2 WHERE f2.content_hash=f.content_hash)
 AND (?='' OR instr(lower(f.original_name),lower(?))>0)
 AND (?=0 OR EXISTS(SELECT 1 FROM album_items a WHERE a.hash=f.content_hash AND a.album_id=?))
 AND (?=0 OR g.favorite=1)`, query, query, album, album, favorite).Scan(&n)
	return n, err
}

func (d *DB) Album(id int64) (*Album, error) {
	a := &Album{ID: id}
	err := d.db.QueryRow("SELECT name FROM albums WHERE id=?", id).Scan(&a.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// Snapshot all image members in one transaction; no UI pagination can truncate delivery.
func (d *DB) QueueAlbum(id int64, device string) (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow("SELECT 1 FROM albums WHERE id=?", id).Scan(&exists); err != nil {
		return 0, err
	}
	result, err := tx.Exec(`INSERT INTO delivery_tasks(hash,device_id,album_id)
 SELECT DISTINCT a.hash,?,a.album_id FROM album_items a JOIN synced_files f ON f.content_hash=a.hash
 WHERE a.album_id=? AND f.media_type='photo'
 ON CONFLICT(hash,device_id,album_id) DO UPDATE SET status='pending',error='',updated=CURRENT_TIMESTAMP`, device, id)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return count, tx.Commit()
}
