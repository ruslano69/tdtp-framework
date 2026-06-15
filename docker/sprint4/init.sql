CREATE SCHEMA IF NOT EXISTS edm;

CREATE TABLE edm.edm_employees (
    id             SERIAL PRIMARY KEY,
    ext_id         VARCHAR(20)  UNIQUE NOT NULL,
    tabel_no       VARCHAR(20),
    display_name   VARCHAR(200),
    hired_at       DATE,
    department     VARCHAR(200),
    job_title      VARCHAR(200),
    contract_type  VARCHAR(30),
    synced_at      TIMESTAMP DEFAULT NOW(),
    correlation_id VARCHAR(50)
);

COMMENT ON TABLE edm.edm_employees IS 'Test table for ZTR-Live → EDM sync (Sprint 4)';
COMMENT ON COLUMN edm.edm_employees.ext_id        IS 'Employee code from ZTR-Live (ZTR$Employee.No_)';
COMMENT ON COLUMN edm.edm_employees.tabel_no      IS 'Timetable number (ZTR$Employment History.No_)';
COMMENT ON COLUMN edm.edm_employees.contract_type IS 'primary / external_part / internal_part / contract';
