# S21 testdata pins — regenerate with gencerts.go, never hand-edit.

These are the sha-256 values a peer configuration's pin field carries
(PINS.md exists so the DEMO walk quotes a file, not a calculation).

| artefact                | kind        | sha-256                                    |
| ----------------------- | ----------- | ------------------------------------------ |
| peer.pem                | certificate | 9c198d52b3236f1949da4c9dd5751a4a19370c2957ccff4c72841882dc57c015 |
| peer.pem                | spki        | d8c8be1b52737063920e401279db5640a9cace6a093e51e330f99de065fdb55a |
| peer-rotated.pem        | certificate | c548a352be7e5489382af24eeece3e0b899547808a04fcf7b0ca9f6118accb26 |
| peer-rotated.pem        | spki        | 892d283a83f0e8a456ed19dbdcb140e08f21c02b550e5efc817b07bb290fc160 |

**The rotation story, on disk:** pin peer's spki, swap the server to
peer-rotated.pem/.key, and the refusal arrives saying "pin no longer
matches" with BOTH values printed — correct until the operator re-reads the
pin from this table after confirming out-of-band. Pinning the certificate
kind instead would fire on renewal too, which is the story the design
refuses to live with (trust.go Q1).
