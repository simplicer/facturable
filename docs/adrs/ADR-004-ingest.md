# ADR-004: Ingest formats

v1: CSV + manual form (universal). XLSX reading deferred to v1.1 (readers are heavy; CSV covers every tool via export). Watch-folder, local HTTP endpoint and E2E relay follow as later increments, in that order: the watch-folder serves non-technical users with no integration at all, the local HTTP endpoint serves scriptable users, and the relay serves SaaS invoicing vendors that cannot run anything on the taxpayer's machine.
