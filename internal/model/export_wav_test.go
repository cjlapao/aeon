package model

// RepairWAVForTest exposes repairWAV to the external test package.
func RepairWAVForTest(b []byte) []byte { return repairWAV(b) }
