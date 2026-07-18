# Project Halcyon — Internal Product Brief

## 1. Project Overview
- **Document Type:** Internal Product Brief
- **Product:** Edge-AI Security Camera
- **Developer:** Northwind Robotics (Headquartered in Tallinn, Estonia)
- **Project Name:** Project Halcyon

## 2. Development Timeline & Team
- **Development Start:** March 2023
- **First Production Shipment:** 14 November 2024
- **Project Lead:** Dr. Ingrid Salu
- **Hardware Workstream Owner:** Marcus Vey
- **Firmware Workstream Owner:** Priya Nandakumar
- **Codename Origin:** Selected after the team rejected the earlier proposal "Kingfisher."

## 3. Hardware Specifications
- **Image Sensor:** 12-megapixel Sony IMX678 (1/1.8-inch optical format)
- **Onboard Compute:** Hailo-8 accelerator (26 TOPS)
- **Power Consumption:** 5.4 W (idle) / 9.1 W (full inference load)
- **Storage:** 256 GB industrial microSD card (rated for 3,000 write cycles)
- **Enclosure Rating:** IP66
- **Operating Temperature:** -30°C to 55°C

## 4. Software & Performance
- **Concurrent Models:** 3 models run simultaneously
- **Person Detection:** YOLOv8n-based model @ 30 FPS
- **License Plate Recognition (LPR):** @ 12 FPS
- **Audio Classification:** Gunshot classifier sampling at 16 kHz
- **Memory:** All three models share a single 8 GB LPDDR4 memory pool

## 5. Pricing & Warranty
- **Base Unit:** $349
- **Pro Tier:** $499 (includes cloud synchronization)
- **Enterprise Tier:** Starts at $12,000 (for a 25-unit deployment)
- **Warranty:** 36-month warranty across all tiers

## 6. 2025 Roadmap
- **Q1 2025:** Release of a thermal-imaging variant
- **Q2 2025:** Shipping of the Halcyon SDK for third-party models
- **Q3 2025:** Targeting EU GDPR compliance certification
- **Note:** Retail pricing for the thermal variant has not been finalized.

## 7. Known Issues & Caveats
- **Thermal Management:** Under continuous gunshot-classifier load, the device occasionally exceeds 60°C. Engineering has not yet confirmed the root cause.
- **Battery Backup:** Estimated runtime is ~4 hours. Field tests have shown a range of 2.5 to 5 hours.
- **LPR Accuracy:** Performance on non-Latin scripts is currently unverified.

## 8. Discrepancies & Unresolved Items
- **Performance Claims:** Marketing materials claim Halcyon is "3x faster" than the competing Verkada CB52. Internal benchmark memos record a 1.8x speedup versus the same competitor.
- **IP Rating:** A reseller reported the device as IP67, while official specifications list it as IP66. This discrepancy remains unresolved.