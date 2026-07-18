## chunk-001

- The document is titled "Project Halcyon — Internal Product Brief".
- The document is an internal product brief.
- The project referenced is named "Project Halcyon".

---

## chunk-002

- Project Halcyon is an edge-AI security camera.
- Project Halcyon is developed by Northwind Robotics.
- Northwind Robotics is headquartered in Tallinn, Estonia.
- Development of Project Halcyon began in March 2023.
- The first production unit shipped on 14 November 2024.
- Dr. Ingrid Salu is the project lead.
- Marcus Vey owns the hardware workstream.
- Priya Nandakumar owns the firmware workstream.
- The codename "Halcyon" was chosen after the team rejected the earlier proposal "Kingfisher."

---

## chunk-003

- Camera uses a 12-megapixel Sony IMX678 sensor
- Sensor has a 1/1.8-inch optical format
- Onboard compute is provided by a Hailo-8 accelerator rated at 26 TOPS
- Device draws 5.4 watts at idle
- Device draws 9.1 watts under full inference load
- Local storage is a 256 GB industrial microSD card
- Storage card is rated for 3000 write cycles
- Enclosure is IP66-rated
- Specified operating temperature range is between -30°C and 55°C

---

## chunk-004

- Halcyon runs three models concurrently.
- A person-detection model based on YOLOv8n runs at 30 frames per second.
- A license-plate recognition model is limited to 12 frames per second.
- An audio gunshot-classifier samples at 16 kHz.
- All three models share a single 8 GB LPDDR4 memory pool.

---

## chunk-005

- Base unit retails for $349.
- Pro tier includes cloud synchronization and costs $499.
- Enterprise pricing starts at $12,000 for a 25-unit deployment.
- Northwind offers a 36-month warranty across all tiers.

---

## chunk-006

- The 2025 roadmap contains four milestones.
- In Q1 2025, the team will add a thermal-imaging variant.
- In Q2 2025, the team will ship the Halcyon SDK for third-party models.
- In Q3 2025, the team targets EU GDPR compliance certification.
- The retail price of the thermal variant has not been finalized.

---

## chunk-007

- Under continuous gunshot-classifier load, the device occasionally overheats above 60°C.
- Engineering has not confirmed the root cause of the overheating.
- Battery-backup runtime is estimated at around 4 hours.
- Field tests for battery-backup runtime have ranged from 2.5 to 5 hours.
- The license-plate model's accuracy on non-Latin scripts is currently unverified.

---

## chunk-008

- Marketing claims Halcyon is "3x faster" than the competing Verkada CB52.
- Internal benchmark memo records a 1.8x speedup for Halcyon versus Verkada CB52.
- A reseller reported Halcyon's IP rating as IP67.
- Official specification lists Halcyon's IP rating as IP66.
- The discrepancy between the reported IP67 and official IP66 rating is unresolved.