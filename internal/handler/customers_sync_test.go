package handler

import (
	"sync"
	"testing"
	"time"

	"github.com/intexa/arca-api/internal/domain"
	"github.com/intexa/arca-api/internal/repository/memory"
	siigopkg "github.com/intexa/arca-api/internal/siigo"
)

func strptr(s string) *string { return &s }

func sampleSiigoCustomers() []siigopkg.Customer {
	return []siigopkg.Customer{
		{
			ID:             "26db4c2c-9685-4fc0-a2ab-ac16ff629c73",
			Type:           "Customer",
			PersonType:     "Company",
			IDType:         siigopkg.IDType{Code: "31", Name: "NIT"},
			Identification: "901698704",
			CheckDigit:     strptr("0"),
			BranchOffice:   0,
			Name:           []string{"GRUPO HOTELES DEL CARIBE S.A.S"},
			Active:         true,
			Address: siigopkg.CustomerAddress{
				Address: "CRA 33  48 109 L 101-104",
				City:    siigopkg.City{CityName: "Bucaramanga", StateName: "Santander", CountryName: "Colombia"},
			},
			// Siigo pads a missing number with zeros — worthless as a phone.
			Phones: []siigopkg.CustomerPhone{{Indicative: "0000", Number: "0000000"}},
			Contacts: []siigopkg.CustomerContact{{
				FirstName: "GRUPO HOTELES DEL CARIBE S.A.S",
				Email:     "grupohotelesdelcaribesas@gmail.com",
				Phone:     siigopkg.CustomerPhone{Number: "3006849047"},
			}},
			Metadata: siigopkg.CustomerMetadata{Created: "2026-09-09T16:13:39.617"},
		},
		{
			ID:             "b7c791d3-b49b-4944-bdaf-36edfc134e48",
			Type:           "Supplier",
			PersonType:     "Person",
			IDType:         siigopkg.IDType{Code: "13", Name: "Cédula de ciudadanía"},
			Identification: "13832081",
			BranchOffice:   1,
			Name:           []string{"Alfredo", "Perez"},
			CommercialName: strptr("Comercializadora AP"),
			Active:         true,
		},
	}
}

func TestSaveCustomersMapsSiigoPayload(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	result := &domain.SiigoSyncResult{}
	var mu sync.Mutex

	if err := h.saveCustomers(sampleSiigoCustomers(), result, &mu); err != nil {
		t.Fatalf("saveCustomers: %v", err)
	}
	if result.CustomersImported != 2 || result.Updated != 0 {
		t.Fatalf("counters: imported=%d updated=%d, want 2/0", result.CustomersImported, result.Updated)
	}

	all, err := store.GetAllCustomers()
	if err != nil {
		t.Fatalf("GetAllCustomers: %v", err)
	}
	byID := map[string]*domain.Customer{}
	for _, c := range all {
		byID[c.Identification] = c
	}

	company := byID["901698704"]
	if company == nil {
		t.Fatal("company not stored")
	}
	if company.Type != domain.CustomerTypeCustomer {
		t.Errorf("type: got %q, want Cliente", company.Type)
	}
	if company.Name != "GRUPO HOTELES DEL CARIBE S.A.S" {
		t.Errorf("name: got %q", company.Name)
	}
	if company.City != "Bucaramanga" || company.State != "Santander" {
		t.Errorf("address: got city=%q state=%q", company.City, company.State)
	}
	if company.Email != "grupohotelesdelcaribesas@gmail.com" {
		t.Errorf("email: got %q", company.Email)
	}
	// The 0000000 phone is dropped, so the contact's number is the one shown.
	if company.Phone != "3006849047" {
		t.Errorf("phone: got %q, want the contact number", company.Phone)
	}
	if len(company.Phones) != 0 {
		t.Errorf("all-zero phone should be dropped, got %v", company.Phones)
	}
	if company.ExternalID != "siigo-cus-26db4c2c-9685-4fc0-a2ab-ac16ff629c73" {
		t.Errorf("externalId: got %q", company.ExternalID)
	}

	person := byID["13832081"]
	if person == nil {
		t.Fatal("person not stored")
	}
	if person.Name != "Alfredo Perez" {
		t.Errorf("split name should rejoin: got %q", person.Name)
	}
	if person.Type != domain.CustomerTypeSupplier {
		t.Errorf("type: got %q, want Proveedor", person.Type)
	}
	if person.CommercialName != "Comercializadora AP" {
		t.Errorf("commercialName: got %q", person.CommercialName)
	}
}

// Re-running a sync must update the existing rows, never add a second copy —
// including when Siigo hands back a different record UUID for the same third
// party, which is why the upsert keys on identification + branch office.
func TestSaveCustomersDoesNotDuplicateOnResync(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	var mu sync.Mutex

	first := &domain.SiigoSyncResult{}
	if err := h.saveCustomers(sampleSiigoCustomers(), first, &mu); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	second := sampleSiigoCustomers()
	second[0].ID = "ffffffff-0000-0000-0000-ffffffffffff" // Siigo reissued the id
	second[0].Name = []string{"GRUPO HOTELES DEL CARIBE SAS"}
	rerun := &domain.SiigoSyncResult{}
	if err := h.saveCustomers(second, rerun, &mu); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if rerun.CustomersImported != 0 || rerun.Updated != 2 {
		t.Errorf("re-sync counters: imported=%d updated=%d, want 0/2",
			rerun.CustomersImported, rerun.Updated)
	}

	all, err := store.GetAllCustomers()
	if err != nil {
		t.Fatalf("GetAllCustomers: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("re-sync duplicated rows: got %d customers, want 2", len(all))
	}

	seen := map[string]bool{}
	for _, c := range all {
		key := c.Identification + "#" + string(rune('0'+c.BranchOffice))
		if seen[key] {
			t.Errorf("duplicate third party for %s branch %d", c.Identification, c.BranchOffice)
		}
		seen[key] = true
		if c.Identification == "901698704" {
			if c.Name != "GRUPO HOTELES DEL CARIBE SAS" {
				t.Errorf("row should carry the updated name, got %q", c.Name)
			}
			if c.SiigoID != "ffffffff-0000-0000-0000-ffffffffffff" {
				t.Errorf("row should track the new Siigo id, got %q", c.SiigoID)
			}
		}
	}
}

// The same NIT at two branch offices is two real third parties in Siigo, so
// collapsing them would lose one.
func TestSaveCustomersKeepsBranchOfficesApart(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	var mu sync.Mutex

	branches := []siigopkg.Customer{
		{ID: "a", Type: "Customer", Identification: "901037916", BranchOffice: 0, Name: []string{"Fosyga"}, Active: true},
		{ID: "b", Type: "Other", Identification: "901037916", BranchOffice: 1, Name: []string{"Fosyga Régimen de Excepción"}, Active: true},
		{ID: "c", Type: "Other", Identification: "901037916", BranchOffice: 2, Name: []string{"Fosyga Residente Exterior"}, Active: true},
	}
	result := &domain.SiigoSyncResult{}
	if err := h.saveCustomers(branches, result, &mu); err != nil {
		t.Fatalf("saveCustomers: %v", err)
	}

	all, err := store.GetAllCustomers()
	if err != nil {
		t.Fatalf("GetAllCustomers: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("branch offices collapsed: got %d rows, want 3", len(all))
	}
}

// A record with no identification has nothing to key on, so it is skipped
// rather than merged into a single blank-identification row.
func TestSaveCustomersSkipsRecordsWithoutIdentification(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	var mu sync.Mutex

	result := &domain.SiigoSyncResult{}
	err := h.saveCustomers([]siigopkg.Customer{
		{ID: "x", Type: "Customer", Identification: "  ", Name: []string{"Sin NIT"}, Active: true},
		{ID: "y", Type: "Customer", Identification: "900123456", Name: []string{"Con NIT"}, Active: true},
	}, result, &mu)
	if err != nil {
		t.Fatalf("saveCustomers: %v", err)
	}

	all, _ := store.GetAllCustomers()
	if len(all) != 1 || all[0].Identification != "900123456" {
		t.Fatalf("blank identification should be skipped, got %d rows", len(all))
	}
	if result.CustomersImported != 1 {
		t.Errorf("imported: got %d, want 1", result.CustomersImported)
	}
}

// A third party that disappears from Siigo is deactivated, not deleted, so the
// invoices and receipts that reference it stay readable. The sweep must leave
// everything the run did touch alone.
func TestSweepDeactivatesOnlyCustomersMissingFromACleanRun(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	var mu sync.Mutex

	if err := h.saveCustomers(sampleSiigoCustomers(), &domain.SiigoSyncResult{}, &mu); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// A later run in which Siigo no longer returns the supplier.
	startedAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	remaining := sampleSiigoCustomers()[:1]
	if err := h.saveCustomers(remaining, &domain.SiigoSyncResult{}, &mu); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	h.sweepCustomers(startedAt, true)

	all, err := store.GetAllCustomers()
	if err != nil {
		t.Fatalf("GetAllCustomers: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("sweep must not delete rows: got %d, want 2", len(all))
	}
	for _, c := range all {
		switch c.Identification {
		case "901698704":
			if !c.Active {
				t.Error("customer still returned by Siigo was deactivated")
			}
		case "13832081":
			if c.Active {
				t.Error("customer missing from Siigo should have been deactivated")
			}
		}
	}
}

// With a failed page the absent rows are an artefact of the failure, so nothing
// may be deactivated on that basis.
func TestSweepSkippedWhenAPageFailed(t *testing.T) {
	store := memory.New()
	h := NewSiigoHandler(store)
	var mu sync.Mutex

	if err := h.saveCustomers(sampleSiigoCustomers(), &domain.SiigoSyncResult{}, &mu); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	startedAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	h.sweepCustomers(startedAt, false) // a page had errored

	all, _ := store.GetAllCustomers()
	for _, c := range all {
		if !c.Active {
			t.Errorf("partial sync must not deactivate %s", c.Identification)
		}
	}
}
