# Project Halcyon — Internal Product Brief

## Overview
Project Halcyon is an edge-AI security camera developed by Northwind Robotics, a company headquartered in Tallinn, Estonia. Development began in March 2023, and the first production unit shipped on 14 November 2024. The project lead is Dr. Ingrid Salu; the hardware workstream is owned by Marcus Vey, and the firmware workstream by Priya Nandakumar. The codename "Halcyon" was chosen after the team rejected an earlier proposal, "Kingfisher."

## Hardware Specifications
The camera uses a 12-megapixel Sony IMX678 sensor with a 1/1.8-inch optical format. Onboard compute is provided by a Hailo-8 accelerator rated at 26 TOPS. The device draws 5.4 watts at idle and 9.1 watts under full inference load. Local storage is a 256 GB industrial microSD card rated for 3000 write cycles. The enclosure is IP66-rated and is specified to operate between -30°C and 55°C.

## On-Device Models
Halcyon runs three models concurrently. The first is a person-detection model based on YOLOv8n running at 30 frames per second. The second is a license-plate recognition model, which is limited to 12 frames per second. The third is an audio gunshot-classifier that samples at 16 kHz. All three models share a single 8 GB LPDDR4 memory pool.

## Pricing and Tiers
The base unit retails for $349. A Pro tier adds cloud synchronization and costs $499. Enterprise pricing starts at $12,000 for a 25-unit deployment. Northwind offers a 36-month warranty across all tiers.

## Roadmap
The 2025 roadmap has four milestones. In Q1 2025 the team will add a thermal-imaging variant. In Q2 2025 they will ship the Halcyon SDK for third-party models. In Q3 2025 they target EU GDPR compliance certification. The thermal variant's retail price has not been finalized.

## Known Issues and Caveats
Under continuous gunshot-classifier load, the device occasionally overheats above 60°C; engineering has not confirmed the root cause. Battery-backup runtime is estimated at around 4 hours, but field tests have ranged from 2.5 to 5 hours. The license-plate model's accuracy on non-Latin scripts is currently unverified.

## Competitive Notes
Marketing claims Halcyon is "3x faster" than the competing Verkada CB52; however, the internal benchmark memo records only a 1.8x speedup. A reseller reported the IP rating as IP67, contradicting the official IP66 specification, and the discrepancy is unresolved.
