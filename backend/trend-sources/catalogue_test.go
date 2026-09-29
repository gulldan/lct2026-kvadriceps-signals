package main

import (
	"strings"
	"testing"
)

func TestCNCFRowsKeepOnlyAcceptedProjects(t *testing.T) {
	body := []byte(`landscape:
  - category:
    name: Provisioning
    subcategories:
      - subcategory:
        name: Automation
        items:
          - item:
            name: Akri
            homepage_url: https://docs.akri.sh
            project: sandbox
            description: A Kubernetes Resource Interface for the Edge
            extra:
              accepted: '2026-03-14'
          - item:
            name: Ansible
            homepage_url: https://www.ansible.com/
          - item:
            name: Later
            homepage_url: https://later.example
            project: incubating
            extra:
              accepted: '2027-01-01'
`)
	rows, err := cncfRows(body, webSource{Name: "CNCF"}, "2026-09-16")
	if err != nil {
		t.Fatalf("разбор каталога: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ожидался один принятый проект в окне, получено %d: %+v", len(rows), rows)
	}
	if rows[0].Date != "2026-03-14" || !strings.Contains(rows[0].Title, "Akri") {
		t.Fatalf("запись собрана неверно: %+v", rows[0])
	}
	if !strings.Contains(rows[0].Text, "sandbox") {
		t.Fatalf("уровень зрелости потерян: %q", rows[0].Text)
	}
}

func TestTechPortRecordUsesFundingStart(t *testing.T) {
	body := []byte(`{"project":{"projectId":183927,"title":"Full-Scale Trash-to-Gas System",
	"description":"Build and test a flight-forward system.","startYear":2025,"startMonth":11,
	"leadOrganization":{"organizationName":"Kennedy Space Center"}}}`)
	row, err := techportRecord(body, webSource{Name: "NASA TechPort"}, "2026-09-16")
	if err != nil {
		t.Fatalf("разбор карточки: %v", err)
	}
	if row.Date != "2025-11-01" || row.URL != "https://techport.nasa.gov/projects/183927" {
		t.Fatalf("карточка собрана неверно: %+v", row)
	}
	if !strings.Contains(row.Text, "Kennedy") {
		t.Fatalf("ведущая организация потеряна: %q", row.Text)
	}
	if _, err := techportRecord(body, webSource{Name: "x"}, "2025-01-01"); err == nil {
		t.Fatal("проект позже границы окна принят")
	}
}
