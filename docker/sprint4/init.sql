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
    birth_date     DATE,
    sex            SMALLINT,
    work_years     NUMERIC(8,2),
    is_active      BOOLEAN,
    meta_json      TEXT,
    synced_at      TIMESTAMP DEFAULT NOW(),
    correlation_id VARCHAR(50)
);

COMMENT ON TABLE edm.edm_employees IS 'Test table for ZTR-Live → EDM sync (Sprint 4)';
COMMENT ON COLUMN edm.edm_employees.ext_id        IS 'Employee code from ZTR-Live (ZTR$Employee.No_)';
COMMENT ON COLUMN edm.edm_employees.tabel_no      IS 'Timetable number (ZTR$Employment History.No_)';
COMMENT ON COLUMN edm.edm_employees.contract_type IS 'primary / external_part / internal_part / contract';
COMMENT ON COLUMN edm.edm_employees.birth_date    IS 'Date of birth (ZTR$Employee.Birth Date)';
COMMENT ON COLUMN edm.edm_employees.sex           IS '1=male 2=female (ZTR$Employee.Sex)';
COMMENT ON COLUMN edm.edm_employees.work_years    IS 'Years of service (computed)';
COMMENT ON COLUMN edm.edm_employees.is_active     IS 'True when employment record is active';
COMMENT ON COLUMN edm.edm_employees.meta_json     IS 'Source metadata JSON blob';
