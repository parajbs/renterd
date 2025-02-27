package stores

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	"go.sia.tech/renterd/hostdb"
	"go.sia.tech/siad/modules"
	"gorm.io/gorm"
)

func (s *SQLStore) insertTestAnnouncement(hk types.PublicKey, a hostdb.Announcement) error {
	return insertAnnouncements(s.db, []announcement{
		{
			hostKey:      publicKey(hk),
			announcement: a,
		},
	})
}

// TestSQLHostDB tests the basic functionality of SQLHostDB using an in-memory
// SQLite DB.
func TestSQLHostDB(t *testing.T) {
	hdb, dbName, ccid, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	if ccid != modules.ConsensusChangeBeginning {
		t.Fatal("wrong ccid", ccid, modules.ConsensusChangeBeginning)
	}

	// Try to fetch a random host. Should fail.
	ctx := context.Background()
	hk := types.GeneratePrivateKey().PublicKey()
	_, err = hdb.Host(ctx, hk)
	if !errors.Is(err, ErrHostNotFound) {
		t.Fatal(err)
	}

	// Add the host
	err = hdb.addTestHost(hk)
	if err != nil {
		t.Fatal(err)
	}

	// Assert it's returned
	allHosts, err := hdb.Hosts(ctx, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(allHosts) != 1 || allHosts[0].PublicKey != hk {
		t.Fatal("unexpected result", len(allHosts))
	}

	// Insert an announcement for the host and another one for an unknown
	// host.
	a := hostdb.Announcement{
		Index: types.ChainIndex{
			Height: 42,
			ID:     types.BlockID{1, 2, 3},
		},
		Timestamp:  time.Now().UTC().Round(time.Second),
		NetAddress: "address",
	}
	err = hdb.insertTestAnnouncement(hk, a)
	if err != nil {
		t.Fatal(err)
	}

	// Read the host and verify that the announcement related fields were
	// set.
	var h dbHost
	tx := hdb.db.Where("last_announcement = ? AND net_address = ?", a.Timestamp, a.NetAddress).Find(&h)
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	if types.PublicKey(h.PublicKey) != hk {
		t.Fatal("wrong host returned")
	}

	// Same thing again but with hosts.
	hosts, err := hdb.hosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatal("wrong number of hosts", len(hosts))
	}
	h1 := hosts[0]
	if !reflect.DeepEqual(h1, h) {
		fmt.Println(h1)
		fmt.Println(h)
		t.Fatal("mismatch")
	}

	// Same thing again but with Host.
	h2, err := hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if h2.NetAddress != h.NetAddress {
		t.Fatal("wrong net address")
	}
	if h2.KnownSince.IsZero() {
		t.Fatal("known since not set")
	}

	// Insert another announcement for an unknown host.
	unknownKey := types.PublicKey{1, 4, 7}
	err = hdb.insertTestAnnouncement(unknownKey, a)
	if err != nil {
		t.Fatal(err)
	}
	h3, err := hdb.Host(ctx, unknownKey)
	if err != nil {
		t.Fatal(err)
	}
	if h3.NetAddress != a.NetAddress {
		t.Fatal("wrong net address")
	}
	if h3.KnownSince.IsZero() {
		t.Fatal("known since not set")
	}

	// Wait for the persist interval to pass to make sure an empty consensus
	// change triggers a persist.
	time.Sleep(testPersistInterval)

	// Apply a consensus change.
	ccid2 := modules.ConsensusChangeID{1, 2, 3}
	hdb.ProcessConsensusChange(modules.ConsensusChange{
		ID: ccid2,
	})

	// Connect to the same DB again.
	conn2 := NewEphemeralSQLiteConnection(dbName)
	hdb2, ccid, err := NewSQLStore(conn2, false, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ccid != ccid2 {
		t.Fatal("ccid wasn't updated", ccid, ccid2)
	}
	_, err = hdb2.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
}

// TestRecordInteractions is a test for RecordInteractions.
func TestRecordInteractions(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	defer hdb.Close()

	// Add a host.
	hk := types.GeneratePrivateKey().PublicKey()
	err = hdb.addTestHost(hk)
	if err != nil {
		t.Fatal(err)
	}

	// It shouldn't have any interactions.
	ctx := context.Background()
	host, err := hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions != (hostdb.Interactions{}) {
		t.Fatal("mismatch")
	}

	createInteractions := func(hk types.PublicKey, successful, failed int) (interactions []hostdb.Interaction) {
		for i := 0; i < successful+failed; i++ {
			interactions = append(interactions, hostdb.Interaction{
				Host:      hk,
				Result:    []byte{1, 2, 3},
				Success:   i < successful,
				Timestamp: time.Now(),
				Type:      "test",
			})
		}
		return
	}

	// Add one successful and two failed interactions.
	if err := hdb.RecordInteractions(ctx, createInteractions(hk, 1, 2)); err != nil {
		t.Fatal(err)
	}
	host, err = hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions != (hostdb.Interactions{
		SuccessfulInteractions: 1,
		FailedInteractions:     2,
	}) {
		t.Fatal("mismatch")
	}

	// Add some more
	if err := hdb.RecordInteractions(ctx, createInteractions(hk, 3, 10)); err != nil {
		t.Fatal(err)
	}
	host, err = hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions != (hostdb.Interactions{
		SuccessfulInteractions: 4,
		FailedInteractions:     12,
	}) {
		t.Fatal("mismatch")
	}

	// Check that interactions were created.
	var interactions []dbInteraction
	if err := hdb.db.Find(&interactions).Error; err != nil {
		t.Fatal(err)
	}
	if len(interactions) != 16 {
		t.Fatal("wrong number of interactions")
	}
	for _, interaction := range interactions {
		if !bytes.Equal(interaction.Result, []byte{1, 2, 3}) {
			t.Fatal("result mismatch")
		}
		if interaction.Timestamp.IsZero() {
			t.Fatal("timestamp not set")
		}
		if interaction.Type != "test" {
			t.Fatal("type not set")
		}
		if types.PublicKey(interaction.Host) != hk {
			t.Fatal("wrong host")
		}
	}
}

func (s *SQLStore) addTestScan(hk types.PublicKey, t time.Time, err error, settings rhpv2.HostSettings) error {
	var sr hostdb.ScanResult
	if err == nil {
		sr.Settings = settings
	} else {
		sr.Error = err.Error()
	}
	result, _ := json.Marshal(sr)
	return s.RecordInteractions(context.Background(), []hostdb.Interaction{
		{
			Host:      hk,
			Result:    result,
			Success:   err == nil,
			Timestamp: t,
			Type:      hostdb.InteractionTypeScan,
		},
	})
}

// TestSQLHosts tests the Hosts method of the SQLHostDB type.
func TestSQLHosts(t *testing.T) {
	db, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	hks, err := db.addTestHosts(3)
	if err != nil {
		t.Fatal(err)
	}
	hk1, hk2, hk3 := hks[0], hks[1], hks[2]

	// assert the hosts method returns the expected hosts
	if hosts, err := db.Hosts(ctx, 0, -1); err != nil || len(hosts) != 3 {
		t.Fatal("unexpected", len(hosts), err)
	}
	if hosts, err := db.Hosts(ctx, 0, 1); err != nil || len(hosts) != 1 {
		t.Fatal("unexpected", len(hosts), err)
	} else if host := hosts[0]; host.PublicKey != hk1 {
		t.Fatal("unexpected host", hk1, hk2, hk3, host.PublicKey)
	}
	if hosts, err := db.Hosts(ctx, 1, 1); err != nil || len(hosts) != 1 {
		t.Fatal("unexpected", len(hosts), err)
	} else if host := hosts[0]; host.PublicKey != hk2 {
		t.Fatal("unexpected host", hk1, hk2, hk3, host.PublicKey)
	}
	if hosts, err := db.Hosts(ctx, 3, 1); err != nil || len(hosts) != 0 {
		t.Fatal("unexpected", len(hosts), err)
	}
	if _, err := db.Hosts(ctx, -1, -1); err != ErrNegativeOffset {
		t.Fatal("unexpected error", err)
	}

	// Add a scan for each host.
	n := time.Now()
	if err := db.addTestScan(hk1, n.Add(-time.Minute), nil, rhpv2.HostSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := db.addTestScan(hk2, n.Add(-2*time.Minute), nil, rhpv2.HostSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := db.addTestScan(hk3, n.Add(-3*time.Minute), nil, rhpv2.HostSettings{}); err != nil {
		t.Fatal(err)
	}

	// Fetch all hosts using the HostsForScanning method.
	hostAddresses, err := db.HostsForScanning(ctx, n, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostAddresses) != 3 {
		t.Fatal("wrong number of addresses")
	}
	if hostAddresses[0].PublicKey != hk3 {
		t.Fatal("wrong key")
	}
	if hostAddresses[1].PublicKey != hk2 {
		t.Fatal("wrong key")
	}
	if hostAddresses[2].PublicKey != hk1 {
		t.Fatal("wrong key")
	}

	// Fetch one host by setting the cutoff exactly to hk2.
	hostAddresses, err = db.HostsForScanning(ctx, n.Add(-2*time.Minute), 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostAddresses) != 1 {
		t.Fatal("wrong number of addresses")
	}

	// Fetch no hosts.
	hostAddresses, err = db.HostsForScanning(ctx, time.Time{}, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostAddresses) != 0 {
		t.Fatal("wrong number of addresses")
	}
}

// TestSearchHosts is a unit test for SearchHosts.
func TestSearchHosts(t *testing.T) {
	db, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// add 3 hosts
	var hks []types.PublicKey
	for i := 0; i < 3; i++ {
		if err := db.addCustomTestHost(types.PublicKey{byte(i)}, fmt.Sprintf("-%v-", i+1)); err != nil {
			t.Fatal(err)
		}
		hks = append(hks, types.PublicKey{byte(i)})
	}
	hk1, hk2, hk3 := hks[0], hks[1], hks[2]

	// Search by address.
	if hosts, err := db.SearchHosts(ctx, 0, -1, hostFilterModeAll, "1", nil); err != nil || len(hosts) != 1 {
		t.Fatal("unexpected", len(hosts), err)
	}
	// Filter by key.
	if hosts, err := db.SearchHosts(ctx, 0, -1, hostFilterModeAll, "", []types.PublicKey{hk1, hk2}); err != nil || len(hosts) != 2 {
		t.Fatal("unexpected", len(hosts), err)
	}
	// Filter by address and key.
	if hosts, err := db.SearchHosts(ctx, 0, -1, hostFilterModeAll, "1", []types.PublicKey{hk1, hk2}); err != nil || len(hosts) != 1 {
		t.Fatal("unexpected", len(hosts), err)
	}
	// Filter by key and limit results
	if hosts, err := db.SearchHosts(ctx, 0, 1, hostFilterModeAll, "3", []types.PublicKey{hk3}); err != nil || len(hosts) != 1 {
		t.Fatal("unexpected", len(hosts), err)
	}
}

// TestRecordScan is a test for recording scans.
func TestRecordScan(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	defer hdb.Close()

	// Add a host.
	hk := types.GeneratePrivateKey().PublicKey()
	err = hdb.addTestHost(hk)
	if err != nil {
		t.Fatal(err)
	}

	// The host shouldn't have any interaction related fields set.
	ctx := context.Background()
	host, err := hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions != (hostdb.Interactions{}) {
		t.Fatal("mismatch")
	}
	if host.Settings != nil {
		t.Fatal("mismatch")
	}

	// Fetch the host directly to get the creation time.
	h, err := hostByPubKey(hdb.db, hk)
	if err != nil {
		t.Fatal(err)
	}
	if h.CreatedAt.IsZero() {
		t.Fatal("creation time not set")
	}

	// Record a scan.
	firstScanTime := time.Now().UTC()
	settings := rhpv2.HostSettings{NetAddress: "host.com"}
	if err := hdb.RecordInteractions(ctx, []hostdb.Interaction{newTestScan(hk, firstScanTime, settings, true)}); err != nil {
		t.Fatal(err)
	}
	host, err = hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}

	// We expect no uptime or downtime from only a single scan.
	uptime := time.Duration(0)
	downtime := time.Duration(0)
	if host.Interactions.LastScan.UnixNano() != firstScanTime.UnixNano() {
		t.Fatal("wrong time")
	}
	host.Interactions.LastScan = time.Time{}
	if host.Interactions != (hostdb.Interactions{
		TotalScans:              1,
		LastScan:                time.Time{},
		LastScanSuccess:         true,
		SecondToLastScanSuccess: false,
		Uptime:                  uptime,
		Downtime:                downtime,
		SuccessfulInteractions:  1,
		FailedInteractions:      0,
	}) {
		t.Fatal("mismatch")
	}
	if !reflect.DeepEqual(*host.Settings, settings) {
		t.Fatal("mismatch")
	}

	// Record another scan 1 hour after the previous one.
	secondScanTime := firstScanTime.Add(time.Hour)
	if err := hdb.RecordInteractions(ctx, []hostdb.Interaction{newTestScan(hk, secondScanTime, settings, true)}); err != nil {
		t.Fatal(err)
	}
	host, err = hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions.LastScan.UnixNano() != secondScanTime.UnixNano() {
		t.Fatal("wrong time")
	}
	host.Interactions.LastScan = time.Time{}
	uptime += secondScanTime.Sub(firstScanTime)
	if host.Interactions != (hostdb.Interactions{
		TotalScans:              2,
		LastScan:                time.Time{},
		LastScanSuccess:         true,
		SecondToLastScanSuccess: true,
		Uptime:                  uptime,
		Downtime:                downtime,
		SuccessfulInteractions:  2,
		FailedInteractions:      0,
	}) {
		t.Fatal("mismatch")
	}

	// Record another scan 2 hours after the second one. This time it fails.
	thirdScanTime := secondScanTime.Add(2 * time.Hour)
	if err := hdb.RecordInteractions(ctx, []hostdb.Interaction{newTestScan(hk, thirdScanTime, settings, false)}); err != nil {
		t.Fatal(err)
	}
	host, err = hdb.Host(ctx, hk)
	if err != nil {
		t.Fatal(err)
	}
	if host.Interactions.LastScan.UnixNano() != thirdScanTime.UnixNano() {
		t.Fatal("wrong time")
	}
	host.Interactions.LastScan = time.Time{}
	downtime += thirdScanTime.Sub(secondScanTime)
	if host.Interactions != (hostdb.Interactions{
		TotalScans:              3,
		LastScan:                time.Time{},
		LastScanSuccess:         false,
		SecondToLastScanSuccess: true,
		Uptime:                  uptime,
		Downtime:                downtime,
		SuccessfulInteractions:  2,
		FailedInteractions:      1,
	}) {
		t.Fatal("mismatch")
	}
}

func TestRemoveHosts(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}
	defer hdb.Close()

	// add a host
	hk := types.GeneratePrivateKey().PublicKey()
	err = hdb.addTestHost(hk)
	if err != nil {
		t.Fatal(err)
	}

	// fetch the host and assert the recent downtime is zero
	h, err := hostByPubKey(hdb.db, hk)
	if err != nil {
		t.Fatal(err)
	}
	if h.RecentDowntime != 0 {
		t.Fatal("downtime is not zero")
	}

	// assert no hosts are removed
	removed, err := hdb.RemoveOfflineHosts(context.Background(), 0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal("expected no hosts to be removed")
	}

	now := time.Now().UTC()
	t1 := now.Add(-time.Minute * 120) // 2 hours ago
	t2 := now.Add(-time.Minute * 90)  // 1.5 hours ago (30min downtime)
	hi1 := newTestScan(hk, t1, rhpv2.HostSettings{NetAddress: "host.com"}, false)
	hi2 := newTestScan(hk, t2, rhpv2.HostSettings{NetAddress: "host.com"}, false)

	// record interactions
	if err := hdb.RecordInteractions(context.Background(), []hostdb.Interaction{hi1, hi2}); err != nil {
		t.Fatal(err)
	}

	// fetch the host and assert the recent downtime is 30 minutes and he has 2 recent scan failures
	h, err = hostByPubKey(hdb.db, hk)
	if err != nil {
		t.Fatal(err)
	}
	if h.RecentDowntime.Minutes() != 30 {
		t.Fatal("downtime is not 30 minutes", h.RecentDowntime.Minutes())
	}
	if h.RecentScanFailures != 2 {
		t.Fatal("recent scan failures is not 2", h.RecentScanFailures)
	}

	// assert no hosts are removed
	removed, err = hdb.RemoveOfflineHosts(context.Background(), 0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal("expected no hosts to be removed")
	}

	// record interactions
	t3 := now.Add(-time.Minute * 60) // 1 hour ago (60min downtime)
	hi3 := newTestScan(hk, t3, rhpv2.HostSettings{NetAddress: "host.com"}, false)
	if err := hdb.RecordInteractions(context.Background(), []hostdb.Interaction{hi3}); err != nil {
		t.Fatal(err)
	}

	// assert no hosts are removed at 61 minutes
	removed, err = hdb.RemoveOfflineHosts(context.Background(), 0, time.Minute*61)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal("expected no hosts to be removed")
	}

	// assert no hosts are removed at 60 minutes if we require at least 4 failed scans
	removed, err = hdb.RemoveOfflineHosts(context.Background(), 4, time.Minute*60)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal("expected no hosts to be removed")
	}

	// assert hosts gets removed at 60 minutes if we require at least 3 failed scans
	removed, err = hdb.RemoveOfflineHosts(context.Background(), 3, time.Minute*60)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatal("expected 1 host to be removed")
	}

	// assert host is removed from the database
	if _, err = hostByPubKey(hdb.db, hk); err != gorm.ErrRecordNotFound {
		t.Fatal("expected record not found error")
	}
}

// TestInsertAnnouncements is a test for insertAnnouncements.
func TestInsertAnnouncements(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}

	// Create announcements for 2 hosts.
	ann1 := announcement{
		hostKey: publicKey(types.GeneratePrivateKey().PublicKey()),
		announcement: hostdb.Announcement{
			Index:      types.ChainIndex{Height: 1, ID: types.BlockID{1}},
			Timestamp:  time.Now(),
			NetAddress: "foo.bar:1000",
		},
	}
	ann2 := announcement{
		hostKey:      publicKey(types.GeneratePrivateKey().PublicKey()),
		announcement: hostdb.Announcement{},
	}
	ann3 := announcement{
		hostKey:      publicKey(types.GeneratePrivateKey().PublicKey()),
		announcement: hostdb.Announcement{},
	}

	// Insert the first one and check that all fields are set.
	if err := insertAnnouncements(hdb.db, []announcement{ann1}); err != nil {
		t.Fatal(err)
	}
	var ann dbAnnouncement
	if err := hdb.db.Find(&ann).Error; err != nil {
		t.Fatal(err)
	}
	ann.Model = Model{} // ignore
	expectedAnn := dbAnnouncement{
		HostKey:     ann1.hostKey,
		BlockHeight: 1,
		BlockID:     types.BlockID{1}.String(),
		NetAddress:  "foo.bar:1000",
	}
	if ann != expectedAnn {
		t.Fatal("mismatch")
	}
	// Insert the first and second one.
	if err := insertAnnouncements(hdb.db, []announcement{ann1, ann2}); err != nil {
		t.Fatal(err)
	}

	// Insert the first one twice. The second one again and the third one.
	if err := insertAnnouncements(hdb.db, []announcement{ann1, ann2, ann1, ann3}); err != nil {
		t.Fatal(err)
	}

	// There should be 3 hosts in the db.
	hosts, err := hdb.hosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Fatal("invalid number of hosts")
	}

	// There should be 7 announcements total.
	var announcements []dbAnnouncement
	if err := hdb.db.Find(&announcements).Error; err != nil {
		t.Fatal(err)
	}
	if len(announcements) != 7 {
		t.Fatal("invalid number of announcements")
	}

	// Add an entry to the blocklist to block host 1
	entry1 := "foo.bar"
	err = hdb.UpdateHostBlocklistEntries(context.Background(), []string{entry1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Insert multiple announcements for host 1 - this asserts that the UNIQUE
	// constraint on the blocklist table isn't triggered when inserting multiple
	// announcements for a host that's on the blocklist
	if err := insertAnnouncements(hdb.db, []announcement{ann1, ann1}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLHostAllowlist(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	numEntries := func() int {
		t.Helper()
		bl, err := hdb.HostAllowlist(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(bl)
	}

	numHosts := func() int {
		t.Helper()
		hosts, err := hdb.Hosts(ctx, 0, -1)
		if err != nil {
			t.Fatal(err)
		}
		return len(hosts)
	}

	numRelations := func() (cnt int64) {
		t.Helper()
		err = hdb.db.Table("host_allowlist_entry_hosts").Count(&cnt).Error
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	isAllowed := func(hk types.PublicKey) bool {
		t.Helper()
		host, err := hdb.Host(ctx, hk)
		if err != nil {
			t.Fatal(err)
		}
		return !host.Blocked
	}

	// add three hosts
	hks, err := hdb.addTestHosts(3)
	if err != nil {
		t.Fatal(err)
	}
	hk1, hk2, hk3 := hks[0], hks[1], hks[2]

	// assert we can find them
	if numHosts() != 3 {
		t.Fatalf("unexpected number of hosts, %v != 3", numHosts())
	}

	// assert allowlist is empty
	if numEntries() != 0 {
		t.Fatalf("unexpected number of entries in allowlist, %v != 0", numEntries())
	}

	// assert we can add entries to the allowlist
	err = hdb.UpdateHostAllowlistEntries(ctx, []types.PublicKey{hk1, hk2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if numEntries() != 2 {
		t.Fatalf("unexpected number of entries in allowlist, %v != 2", numEntries())
	}
	if numRelations() != 2 {
		t.Fatalf("unexpected number of entries in join table, %v != 2", numRelations())
	}

	// assert both h1 and h2 are allowed now
	if !isAllowed(hk1) || !isAllowed(hk2) || isAllowed(hk3) {
		t.Fatal("unexpected hosts are allowed", isAllowed(hk1), isAllowed(hk2), isAllowed(hk3))
	}

	// assert adding the same entry is a no-op and we can remove an entry at the same time
	err = hdb.UpdateHostAllowlistEntries(ctx, []types.PublicKey{hk1}, []types.PublicKey{hk2})
	if err != nil {
		t.Fatal(err)
	}
	if numEntries() != 1 {
		t.Fatalf("unexpected number of entries in allowlist, %v != 1", numEntries())
	}
	if numRelations() != 1 {
		t.Fatalf("unexpected number of entries in join table, %v != 1", numRelations())
	}

	// assert only h1 is allowed now
	if !isAllowed(hk1) || isAllowed(hk2) || isAllowed(hk3) {
		t.Fatal("unexpected hosts are allowed", isAllowed(hk1), isAllowed(hk2), isAllowed(hk3))
	}

	// assert removing a non-existing entry is a no-op
	err = hdb.UpdateHostAllowlistEntries(ctx, nil, []types.PublicKey{hk2})
	if err != nil {
		t.Fatal(err)
	}

	assertSearch := func(total, allowed, blocked int) error {
		t.Helper()
		hosts, err := hdb.SearchHosts(context.Background(), 0, -1, hostFilterModeAll, "", nil)
		if err != nil {
			return err
		}
		if len(hosts) != total {
			return fmt.Errorf("invalid number of hosts: %v", len(hosts))
		}
		hosts, err = hdb.SearchHosts(context.Background(), 0, -1, hostFilterModeAllowed, "", nil)
		if err != nil {
			return err
		}
		if len(hosts) != allowed {
			return fmt.Errorf("invalid number of hosts: %v", len(hosts))
		}
		hosts, err = hdb.SearchHosts(context.Background(), 0, -1, hostFilterModeBlocked, "", nil)
		if err != nil {
			return err
		}
		if len(hosts) != blocked {
			return fmt.Errorf("invalid number of hosts: %v", len(hosts))
		}
		return nil
	}

	// Search for hosts using different modes. Should have 3 hosts in total, 2
	// allowed ones and 2 blocked ones.
	if err := assertSearch(3, 1, 2); err != nil {
		t.Fatal(err)
	}

	// remove host 1
	if err = hdb.db.Model(&dbHost{}).Where(&dbHost{PublicKey: publicKey(hk1)}).Delete(&dbHost{}).Error; err != nil {
		t.Fatal(err)
	}
	if numHosts() != 0 {
		t.Fatalf("unexpected number of hosts, %v != 0", numHosts())
	}
	if numRelations() != 0 {
		t.Fatalf("unexpected number of entries in join table, %v != 0", numRelations())
	}
	if numEntries() != 1 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 1", numEntries())
	}

	// Search for hosts using different modes. Should have 2 hosts in total, 0
	// allowed ones and 2 blocked ones.
	if err := assertSearch(2, 0, 2); err != nil {
		t.Fatal(err)
	}

	// remove the allowlist entry for h1
	err = hdb.UpdateHostAllowlistEntries(ctx, nil, []types.PublicKey{hk1})
	if err != nil {
		t.Fatal(err)
	}

	// Search for hosts using different modes. Should have 2 hosts in total, 2
	// allowed ones and 0 blocked ones.
	if err := assertSearch(2, 2, 0); err != nil {
		t.Fatal(err)
	}

	// assert hosts reappear
	if numHosts() != 2 {
		t.Fatalf("unexpected number of hosts, %v != 2", numHosts())
	}
	if numEntries() != 0 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 0", numEntries())
	}
}

func TestSQLHostBlocklist(t *testing.T) {
	hdb, _, _, err := newTestSQLStore()
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	numEntries := func() int {
		t.Helper()
		bl, err := hdb.HostBlocklist(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(bl)
	}

	numHosts := func() int {
		t.Helper()
		hosts, err := hdb.Hosts(ctx, 0, -1)
		if err != nil {
			t.Fatal(err)
		}
		return len(hosts)
	}

	numRelations := func() (cnt int64) {
		t.Helper()
		err = hdb.db.Table("host_blocklist_entry_hosts").Count(&cnt).Error
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	isBlocked := func(hk types.PublicKey) bool {
		t.Helper()
		host, _ := hdb.Host(ctx, hk)
		return host.Blocked
	}

	// add three hosts
	hk1 := types.GeneratePrivateKey().PublicKey()
	if err := hdb.addCustomTestHost(hk1, "foo.bar.com:1000"); err != nil {
		t.Fatal(err)
	}
	hk2 := types.GeneratePrivateKey().PublicKey()
	if err := hdb.addCustomTestHost(hk2, "bar.baz.com:2000"); err != nil {
		t.Fatal(err)
	}
	hk3 := types.GeneratePrivateKey().PublicKey()
	if err := hdb.addCustomTestHost(hk3, "foobar.com:3000"); err != nil {
		t.Fatal(err)
	}

	// assert we can find them
	if numHosts() != 3 {
		t.Fatalf("unexpected number of hosts, %v != 3", numHosts())
	}

	// assert blocklist is empty
	if numEntries() != 0 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 0", numEntries())
	}

	// assert we can add entries to the blocklist
	err = hdb.UpdateHostBlocklistEntries(ctx, []string{"foo.bar.com", "bar.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if numEntries() != 2 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 2", numEntries())
	}
	if numRelations() != 2 {
		t.Fatalf("unexpected number of entries in join table, %v != 2", numRelations())
	}

	// assert only host 1 is blocked now
	if !isBlocked(hk1) || isBlocked(hk2) || isBlocked(hk3) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk2), isBlocked(hk3))
	}
	if host, err := hdb.Host(ctx, hk1); err != nil {
		t.Fatal("unexpected err", err)
	} else if !host.Blocked {
		t.Fatal("expected host to be blocked")
	}

	// assert adding the same entry is a no-op, and we can remove entries at the same time
	err = hdb.UpdateHostBlocklistEntries(ctx, []string{"foo.bar.com", "bar.com"}, []string{"foo.bar.com"})
	if err != nil {
		t.Fatal(err)
	}
	if numEntries() != 1 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 1", numEntries())
	}
	if numRelations() != 1 {
		t.Fatalf("unexpected number of entries in join table, %v != 1", numRelations())
	}

	// assert the host is still blocked
	if !isBlocked(hk1) || isBlocked(hk2) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk2))
	}

	// assert removing a non-existing entry is a no-op
	err = hdb.UpdateHostBlocklistEntries(ctx, nil, []string{"foo.bar.com"})
	if err != nil {
		t.Fatal(err)
	}
	// remove the other entry and assert the delete cascaded properly
	err = hdb.UpdateHostBlocklistEntries(ctx, nil, []string{"bar.com"})
	if err != nil {
		t.Fatal(err)
	}
	if numEntries() != 0 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 0", numEntries())
	}
	if isBlocked(hk1) || isBlocked(hk2) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk2))
	}
	if numRelations() != 0 {
		t.Fatalf("unexpected number of entries in join table, %v != 0", numRelations())
	}

	// block the second host
	err = hdb.UpdateHostBlocklistEntries(ctx, []string{"baz.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if isBlocked(hk1) || !isBlocked(hk2) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk2))
	}
	if numRelations() != 1 {
		t.Fatalf("unexpected number of entries in join table, %v != 1", numRelations())
	}

	// delete host 2 and assert the delete cascaded properly
	if err = hdb.db.Model(&dbHost{}).Where(&dbHost{PublicKey: publicKey(hk2)}).Delete(&dbHost{}).Error; err != nil {
		t.Fatal(err)
	}
	if numHosts() != 2 {
		t.Fatalf("unexpected number of hosts, %v != 2", numHosts())
	}
	if numRelations() != 0 {
		t.Fatalf("unexpected number of entries in join table, %v != 0", numRelations())
	}
	if numEntries() != 1 {
		t.Fatalf("unexpected number of entries in blocklist, %v != 1", numEntries())
	}

	// add two hosts, one that should be blocked by 'baz.com' and one that should not
	hk4 := types.GeneratePrivateKey().PublicKey()
	if err := hdb.addCustomTestHost(hk4, "foo.baz.com:3000"); err != nil {
		t.Fatal(err)
	}
	hk5 := types.GeneratePrivateKey().PublicKey()
	if err := hdb.addCustomTestHost(hk5, "foo.baz.commmmm:3000"); err != nil {
		t.Fatal(err)
	}

	if host, err := hdb.Host(ctx, hk4); err != nil {
		t.Fatal(err)
	} else if !host.Blocked {
		t.Fatal("expected host to be blocked")
	}
	if _, err = hdb.Host(ctx, hk5); err != nil {
		t.Fatal("expected host to be found")
	}

	// now update host 4's address so it's no longer blocked
	if err := hdb.addCustomTestHost(hk4, "foo.baz.commmmm:3000"); err != nil {
		t.Fatal(err)
	}
	if _, err = hdb.Host(ctx, hk4); err != nil {
		t.Fatal("expected host to be found")
	}
	if numRelations() != 0 {
		t.Fatalf("unexpected number of entries in join table, %v != 0", numRelations())
	}

	// add another entry that blocks multiple hosts
	err = hdb.UpdateHostBlocklistEntries(ctx, []string{"com"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// assert 2 out of 5 hosts are blocked
	if !isBlocked(hk1) || !isBlocked(hk3) || isBlocked(hk4) || isBlocked(hk5) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk3), isBlocked(hk4), isBlocked(hk5))
	}

	// add host 5 to the allowlist
	err = hdb.UpdateHostAllowlistEntries(ctx, []types.PublicKey{hk5}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// assert all hosts except host 5 are blocked
	if !isBlocked(hk1) || !isBlocked(hk3) || !isBlocked(hk4) || isBlocked(hk5) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk3), isBlocked(hk4), isBlocked(hk5))
	}

	// add a rule to block host 5
	err = hdb.UpdateHostBlocklistEntries(ctx, []string{"foo.baz.commmmm"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// assert all hosts are blocked
	if !isBlocked(hk1) || !isBlocked(hk3) || !isBlocked(hk4) || !isBlocked(hk5) {
		t.Fatal("unexpected host is blocked", isBlocked(hk1), isBlocked(hk3), isBlocked(hk4), isBlocked(hk5))
	}
}

// addTestHosts adds 'n' hosts to the db and returns their keys.
func (s *SQLStore) addTestHosts(n int) (keys []types.PublicKey, err error) {
	cnt, err := s.contractsCount()
	if err != nil {
		return nil, err
	}

	for i := 0; i < n; i++ {
		keys = append(keys, types.PublicKey{byte(int(cnt) + i + 1)})
		if err := s.addTestHost(keys[len(keys)-1]); err != nil {
			return nil, err
		}
	}
	return
}

// addTestHost ensures a host with given hostkey exists.
func (s *SQLStore) addTestHost(hk types.PublicKey) error {
	return s.addCustomTestHost(hk, "")
}

// addCustomTestHost ensures a host with given hostkey and net address exists.
func (s *SQLStore) addCustomTestHost(hk types.PublicKey, na string) error {
	return insertAnnouncements(s.db, []announcement{{
		hostKey:      publicKey(hk),
		announcement: hostdb.Announcement{NetAddress: na},
	}})
}

// hosts returns all hosts in the db. Only used in testing since preloading all
// interactions for all hosts is expensive in production.
func (db *SQLStore) hosts() ([]dbHost, error) {
	var hosts []dbHost
	tx := db.db.Find(&hosts)
	if tx.Error != nil {
		return nil, tx.Error
	}
	return hosts, nil
}

func hostByPubKey(tx *gorm.DB, hostKey types.PublicKey) (dbHost, error) {
	var h dbHost
	err := tx.Where("public_key", publicKey(hostKey)).
		Take(&h).Error
	return h, err
}

// newTestScan returns a host interaction with given parameters.
func newTestScan(hk types.PublicKey, scanTime time.Time, settings rhpv2.HostSettings, success bool) hostdb.Interaction {
	var err string
	if !success {
		err = "failure"
	}
	result, _ := json.Marshal(struct {
		Settings rhpv2.HostSettings `json:"settings"`
		Error    string             `json:"error"`
	}{
		Settings: settings,
		Error:    err,
	})
	return hostdb.Interaction{
		Host:      hk,
		Result:    result,
		Success:   success,
		Timestamp: scanTime,
		Type:      hostdb.InteractionTypeScan,
	}
}
