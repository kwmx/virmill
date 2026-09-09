# Import a VM or installer

Run `virmill`, choose **Import**, then select a file. Enter opens a folder;
**Ctrl+S** selects a folder of disks. The browser also offers **Create folder**.
It starts at your normal user location, not a development test directory.

Virmill reads the source before asking where to save it:

| Source | What the review can show |
| --- | --- |
| OVA appliance | Declared VM name, CPU, memory, OS, disks and hardware hints |
| ISO installer | Disc label and file size; suggested blank VM settings |
| QCOW2, raw/IMG, VMDK, VDI, VHD, VHDX | Actual disk format, capacity and declared dependencies |
| Folder of disks | Supported root disks and dependencies in that folder |

Change **VM name**, **CPU cores** and **Memory** on the review screen. Open
**Advanced settings** for firmware, disk buses, network choices and the optional
**Guest agent channel**. Then choose the destination and review storage needs.
Standalone disks and installers do not declare reliable CPU/RAM/OS settings;
suggestions are labeled. A source-format label does not certify guest bootability.

Keep split VMDK extents and backing files together and select their folder.
Subfolders are not searched. Extract 7z/ZIP and other compressed downloads into a
new folder before selecting the actual disk or OVA. Loose OVF, VMX and VBOX
configuration files cannot be imported as raw disks. Encrypted, inaccessible,
changed or unsupported sources produce a refusal with the next step.

Reading settings is read-only. Preparation still verifies bytes and dependencies;
its plan shows storage requirements before copying. Existing guests and source
files are preserved. VM registration, boot and guest readiness remain separate.

CLI users can read the same metadata:

```sh
virmill import source describe /absolute/path/to/image.qcow2 --output json
virmill import source describe /absolute/path/to/disk-folder --output json
```

After boot, choose the VM's **Guest tools** action. See [Guest tools](guest-tools.md)
for supported systems and the access required for installation.
