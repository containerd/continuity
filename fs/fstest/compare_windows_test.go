/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package fstest

import "testing"

func TestCheckDirectoryEqualMetadataFiles(t *testing.T) {
	base := Apply(
		CreateDir("/d", 0o755),
		CreateFile("/d/f", []byte("hello"), 0o644),
	)
	metadata := Apply(
		CreateDir("/System Volume Information", 0o755),
		CreateDir("/WcSandboxState", 0o755),
		CreateDir("/WcSandboxState/Hives", 0o755),
		CreateFile("/WcSandboxState/Hives/Sam_Delta", []byte("sam"), 0o644),
		CreateFile("/WcSandboxState/initialized", nil, 0o644),
		CreateDir("/Windows", 0o755),
		CreateDir("/Windows/System32", 0o755),
		CreateDir("/Windows/System32/config", 0o755),
		CreateFile("/Windows/System32/config/SYSTEM", []byte("system"), 0o644),
	)

	for _, tc := range []struct {
		name    string
		extra   Applier
		wantErr bool
	}{
		{
			name:  "MetadataIgnored",
			extra: metadata,
		},
		{
			name:    "OtherAdditionDetected",
			extra:   Apply(metadata, CreateFile("/Windows/other", []byte("other"), 0o644)),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d1 := t.TempDir()
			d2 := t.TempDir()
			if err := base.Apply(d1); err != nil {
				t.Fatal(err)
			}
			if err := Apply(base, tc.extra).Apply(d2); err != nil {
				t.Fatal(err)
			}

			err := CheckDirectoryEqual(d1, d2)
			if tc.wantErr && err == nil {
				t.Error("CheckDirectoryEqual() = nil, want error for addition outside metadataFiles")
			} else if !tc.wantErr && err != nil {
				t.Errorf("CheckDirectoryEqual() = %v, want nil", err)
			}
		})
	}
}
