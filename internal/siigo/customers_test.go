package siigo

import (
	"encoding/json"
	"testing"
)

// Trimmed from a live GET /v1/customers response: a company with a full
// address, and a person whose name arrives split across two array elements.
// Both carry the nulls the real payload uses for absent optional fields.
const sampleCustomers = `{
  "pagination": { "page": 1, "page_size": 100, "total_results": 1797 },
  "results": [
    {
      "id": "26db4c2c-9685-4fc0-a2ab-ac16ff629c73",
      "type": "Customer",
      "person_type": "Company",
      "id_type": { "code": "31", "name": "NIT" },
      "identification": "901698704",
      "branch_office": 0,
      "check_digit": "0",
      "name": [ "GRUPO HOTELES DEL CARIBE S.A.S" ],
      "commercial_name": null,
      "active": true,
      "vat_responsible": false,
      "fiscal_responsibilities": [ { "code": "R-99-PN", "name": "No aplica - Otros" } ],
      "address": {
        "address": "CRA 33  48 109 L 101-104",
        "city": { "country_code": "Co", "country_name": "Colombia", "state_code": "68", "state_name": "Santander", "city_code": "68001", "city_name": "Bucaramanga" },
        "postal_code": null
      },
      "phones": [ { "indicative": "0000", "number": "0000000" } ],
      "contacts": [ { "first_name": "GRUPO HOTELES DEL CARIBE S.A.S", "last_name": "", "email": "grupohotelesdelcaribesas@gmail.com", "phone": { "indicative": "0000", "number": "3006849047" } } ],
      "comments": null,
      "metadata": { "created": "2026-09-09T16:13:39.617" }
    },
    {
      "id": "b7c791d3-b49b-4944-bdaf-36edfc134e48",
      "type": "Supplier",
      "person_type": "Person",
      "id_type": { "code": "13", "name": "Cédula de ciudadanía" },
      "identification": "13832081",
      "branch_office": 1,
      "check_digit": null,
      "name": [ "Alfredo", "Perez" ],
      "commercial_name": "Comercializadora AP",
      "active": true,
      "vat_responsible": true,
      "address": { "address": "", "city": {} },
      "phones": [],
      "contacts": [],
      "metadata": { "created": "2024-04-08T21:20:05.243", "last_updated": "2025-11-21T16:32:32.26" }
    }
  ]
}`

func TestCustomerListDecodesRealPayload(t *testing.T) {
	var resp CustomerListResponse
	if err := json.Unmarshal([]byte(sampleCustomers), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Pagination.TotalResults != 1797 || len(resp.Results) != 2 {
		t.Fatalf("pagination: got total=%d results=%d", resp.Pagination.TotalResults, len(resp.Results))
	}

	company := resp.Results[0]
	if company.Type != "Customer" || company.PersonType != "Company" {
		t.Errorf("company classification: got type=%q person_type=%q", company.Type, company.PersonType)
	}
	if len(company.Name) != 1 || company.Name[0] != "GRUPO HOTELES DEL CARIBE S.A.S" {
		t.Errorf("company name: got %v", company.Name)
	}
	if company.IDType.Name != "NIT" {
		t.Errorf("id_type.name: got %q, want NIT", company.IDType.Name)
	}
	if company.CommercialName != nil {
		t.Errorf("null commercial_name should decode to nil, got %q", *company.CommercialName)
	}
	if company.CheckDigit == nil || *company.CheckDigit != "0" {
		t.Errorf("check_digit: got %v, want \"0\"", company.CheckDigit)
	}
	if company.Address.City.CityName != "Bucaramanga" || company.Address.City.StateName != "Santander" {
		t.Errorf("address city: got %+v", company.Address.City)
	}
	if company.Contacts[0].Email != "grupohotelesdelcaribesas@gmail.com" {
		t.Errorf("contact email: got %q", company.Contacts[0].Email)
	}

	person := resp.Results[1]
	if len(person.Name) != 2 || person.Name[0] != "Alfredo" || person.Name[1] != "Perez" {
		t.Errorf("person name parts: got %v", person.Name)
	}
	if person.CheckDigit != nil {
		t.Errorf("null check_digit should decode to nil, got %q", *person.CheckDigit)
	}
	if person.BranchOffice != 1 {
		t.Errorf("branch_office: got %d, want 1", person.BranchOffice)
	}
	if person.Metadata.LastUpdated != "2025-11-21T16:32:32.26" {
		t.Errorf("last_updated: got %q", person.Metadata.LastUpdated)
	}
	// An empty city object must not blow up or invent values.
	if person.Address.City.CityName != "" {
		t.Errorf("empty city should stay empty, got %q", person.Address.City.CityName)
	}
}
