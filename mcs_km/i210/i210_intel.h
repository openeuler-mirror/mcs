#ifndef _I210_INTEL_H_
#define _I210_INTEL_H_

#define INTEL_E1000_CTRL 0x00000 /* Device Control - RW */
/* Device Control */
#define INTEL_E1000_CTRL_FD 0x00000001 /* Full duplex.0=half; 1=full */
#define INTEL_E1000_CTRL_GIO_MASTER_DISABLE 0x00000004 /*Blocks new Master requests */
#define INTEL_E1000_CTRL_LRST 0x00000008 /* Link reset. 0=normal,1=reset */
#define INTEL_E1000_CTRL_ASDE 0x00000020 /* Auto-speed detect enable */
#define INTEL_E1000_CTRL_SLU 0x00000040 /* Set link up (Force Link) */
#define INTEL_E1000_CTRL_ILOS 0x00000080 /* Invert Loss-Of Signal */
#define INTEL_E1000_CTRL_SPD_SEL 0x00000300 /* Speed Select Mask */
#define INTEL_E1000_CTRL_SPD_10 0x00000000 /* Force 10Mb */
#define INTEL_E1000_CTRL_SPD_100 0x00000100 /* Force 100Mb */
#define INTEL_E1000_CTRL_SPD_1000 0x00000200 /* Force 1Gb */
#define INTEL_E1000_CTRL_FRCSPD 0x00000800 /* Force Speed */
#define INTEL_E1000_CTRL_FRCDPX 0x00001000 /* Force Duplex */
#define INTEL_E1000_CTRL_LANPHYPC_OVERRIDE 0x00010000 /* SW control of LANDPHYPC */
#define INTEL_E1000_CTRL_LANPHYPC_VALUE 0x00020000 /* SW value of LANDPHYPC */
#define INTEL_E1000_CTRL_MEHE 0x00080000 /* Memory Error Handling Enable */
#define INTEL_E1000_CTRL_SWDPIN0 0x00040000 /* SWDPIN 0 value */
#define INTEL_E1000_CTRL_SWDPIN1 0x00080000 /* SWDPIN 1 value */
#define INTEL_E1000_CTRL_ADVD3WUC 0x00100000 /* D3 WUC */
#define INTEL_E1000_CTRL_EN_PHY_PWR_MGMT 0x00200000 /* PHY PM enable */
#define INTEL_E1000_CTRL_SWDPIO0 0x00400000 /* SWDPIN 0 Input or output */
#define INTEL_E1000_CTRL_RST 0x04000000 /* Global reset */
#define INTEL_E1000_CTRL_RFCE 0x08000000 /* Receive Flow Control enable */
#define INTEL_E1000_CTRL_TFCE 0x10000000 /* Transmit flow control enable */
#define INTEL_E1000_CTRL_VME 0x40000000 /* IEEE VLAN mode enable */
#define INTEL_E1000_CTRL_PHY_RST 0x80000000 /* PHY Reset */

#define INTEL_E1000_STATUS 0x00008 /* Device Status - RO */
/* Device Status */
#define INTEL_E1000_STATUS_FD 0x00000001 /* Full duplex.0=half,1=full */
#define INTEL_E1000_STATUS_LU 0x00000002 /* Link up.0=no,1=link */
#define INTEL_E1000_STATUS_FUNC_MASK 0x0000000C /* PCI Function Mask */
#define INTEL_E1000_STATUS_FUNC_SHIFT 2
#define INTEL_E1000_STATUS_FUNC_1 0x00000004 /* Function 1 */
#define INTEL_E1000_STATUS_TXOFF 0x00000010 /* transmission paused */
#define INTEL_E1000_STATUS_SPEED_MASK 0x000000C0
#define INTEL_E1000_STATUS_SPEED_10 0x00000000 /* Speed 10Mb/s */
#define INTEL_E1000_STATUS_SPEED_100 0x00000040 /* Speed 100Mb/s */
#define INTEL_E1000_STATUS_SPEED_1000 0x00000080 /* Speed 1000Mb/s */
#define INTEL_E1000_STATUS_LAN_INIT_DONE 0x00000200 /* Lan Init Compltn by NVM */
#define INTEL_E1000_STATUS_PHYRA 0x00000400 /* PHY Reset Asserted */
#define INTEL_E1000_STATUS_GIO_MASTER_ENABLE 0x00080000 /* Master request status */
#define INTEL_E1000_STATUS_2P5_SKU 0x00001000 /* Val of 2.5GBE SKU strap */
#define INTEL_E1000_STATUS_2P5_SKU_OVER 0x00002000 /* Val of 2.5GBE SKU Over */
#define INTEL_E1000_STATUS_PCIM_STATE 0x40000000 /* PCIm function state */

#define INTEL_E1000_STRAP 0x0000C
#define INTEL_E1000_STRAP_SMBUS_ADDRESS_MASK 0x00fe0000
#define INTEL_E1000_STRAP_SMBUS_ADDRESS_SHIFT 17
#define INTEL_E1000_STRAP_SMT_FREQ_MASK 0x00003000
#define INTEL_E1000_STRAP_SMT_FREQ_SHIFT 12

#define INTEL_E1000_EECD 0x00010 /* EEPROM/Flash Control - RW */
#define INTEL_E1000_EERD 0x00014 /* EEPROM Read - RW */
#define INTEL_E1000_CTRL_EXT 0x00018 /* Extended Device Control - RW */
#define INTEL_E1000_CTRL_EXT_LPCD 0x00000004 /* LCD Power Cycle Done */
#define INTEL_E1000_CTRL_EXT_SDP3_DATA 0x00000080 /* Value of SW Defineable Pin 3 */
#define INTEL_E1000_CTRL_EXT_FORCE_SMBUS 0x000800 /* Force SMBus mode */
#define INTEL_E1000_CTRL_EXT_EE_RST 0x00002000 /* Reinitialize from EEPROM */
#define INTEL_E1000_CTRL_EXT_SPD_BYPS 0x00008000 /* Speed Select Bypass */
#define INTEL_E1000_CTRL_EXT_RO_DIS 0x00020000 /* Relaxed Ordering disable */
#define INTEL_E1000_CTRL_EXT_DMA_DYN_CLK_EN 0x00080000 /* DMA Dynamic Clk Gating */
#define INTEL_E1000_CTRL_EXT_LINK_MODE_MASK 0x00C00000
#define INTEL_E1000_CTRL_EXT_LINK_MODE_PCIE_SERDES 0x00c00000
#define INTEL_E1000_CTRL_EXT_EIAME 0x01000000
#define INTEL_E1000_CTRL_EXT_DRV_LOAD 0x10000000 /* Driver loaded bit for FW */
#define INTEL_E1000_CTRL_EXT_IAME 0x08000000 /* Int Ack Auto-mask */
#define INTEL_E1000_CTRL_EXT_PBA_CLR 0x80000000 /* PBA Clear */
#define INTEL_E1000_CTRL_EXT_LSECCK 0x00001000
#define INTEL_E1000_CTRL_EXT_PHYPDEN 0x00100000

#define INTEL_E1000_FLA 0x0001C /* Flash Access - RW */
#define INTEL_E1000_MDIC 0x00020 /* MDI Control - RW */
#define INTEL_E1000_SCTL 0x00024 /* SerDes Control - RW */
#define INTEL_E1000_FCAL 0x00028 /* Flow Control Address Low - RW */
#define INTEL_E1000_FCAH 0x0002C /* Flow Control Address High -RW */
#define INTEL_E1000_FEXT 0x0002C /* Future Externed - RW */
#define INTEL_E1000_FEXTNVM 0x00028 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM3 0x0003C /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM4 0x00024 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM5 0x00014 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM6 0x00010 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM7 0x000E4 /* Future Externed NVM - RW */

#define INTEL_E1000_FEXTNVM8 0x05bb0 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM9 0x05bb4 /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM11 0x05bbc /* Future Externed NVM - RW */
#define INTEL_E1000_FEXTNVM12 0x05bc0 /* Future Externed NVM - RW */
#define INTEL_E1000_PCIEANACFG 0x00F18 /* PCIE Analog config */
#define INTEL_E1000_DPGFR 0x00FAC /* Dynamic Power Gate Force Control Register */
#define INTEL_E1000_FCT 0x00030 /* Flow Control Type - RW */
#define INTEL_E1000_VET 0x00038 /* VLAN Ether Type - RW */
#define INTEL_E1000_ICR 0x000C0 /* Interrupt Cause Read - R/clr */
#define INTEL_E1000_ITR 0x000C4 /* Interrupt Throttling Rate - RW */
#define INTEL_E1000_ICS 0x000C8 /* Interrupt Cause Set - WO */
#define INTEL_E1000_IMS 0x000D0 /* Interrupt Mask Set - RW */
#define INTEL_E1000_IMC 0x000D8 /* Interrupt Mask Clear - WO */
#define INTEL_E1000_IAM 0x000E0 /* Interrupt Acknowledge Auto Mask */
#define INTEL_E1000_IVAR 0x000E4 /* Interrupt Vector Allocation Register - RW */
#define INTEL_E1000_SVCR 0x000F0
#define INTEL_E1000_SVT 0x000F4
#define INTEL_E1000_LPIC 0x000FC /* Low Power IDLE control */
#define INTEL_E1000_RCTL 0x00100 /* RX Control - RW */
#define INTEL_E1000_RCTL_EN 0x00000002 /* enable */
#define INTEL_E1000_RCTL_SBP 0x00000004 /* store bad packet */
#define INTEL_E1000_RCTL_UPE 0x00000008 /* unicast promiscuous enable */
#define INTEL_E1000_RCTL_MPE 0x00000010 /* multicast promiscuous enab */
#define INTEL_E1000_RCTL_LPE 0x00000020 /* long packet enable */
#define INTEL_E1000_RCTL_LBM_NO 0x00000000 /* no loopback mode */
#define INTEL_E1000_RCTL_LBM_MAC 0x00000040 /* MAC loopback mode */
#define INTEL_E1000_RCTL_LBM_TCVR 0x000000C0 /* tcvr loopback mode */
#define INTEL_E1000_RCTL_DTYP_PS 0x00000400 /* Packet Split descriptor */
#define INTEL_E1000_RCTL_RDMTS_HALF 0x00000000 /* rx desc min threshold size */
#define INTEL_E1000_RCTL_RDMTS_HEX 0x00010000
#define INTEL_E1000_RCTL_RDMTS1_HEX INTEL_E1000_RCTL_RDMTS_HEX
#define INTEL_E1000_RCTL_MO_SHIFT 12 /* multicast offset shift */
#define INTEL_E1000_RCTL_MO_3 0x00003000 /* multicast offset 15:4 */
#define INTEL_E1000_RCTL_BAM 0x00008000 /* broadcast enable */
/* these buff sizes are valid if E1000_RCTL_BSEX is 0 */
#define INTEL_E1000_RCTL_SZ_2048 0x00000000 /* rx buffer size 2048 */
#define INTEL_E1000_RCTL_SZ_1024 0x00010000 /* rx buffer size 1024 */
#define INTEL_E1000_RCTL_SZ_512 0x00020000 /* rx buffer size 512 */
#define INTEL_E1000_RCTL_SZ_256 0x00030000 /* rx buffer size 256 */
/* these buff sizes are valid if E1000_RCTL_BSEX is 1 */
#define INTEL_E1000_RCTL_SZ_16384 0x00010000 /* rx buffer size 16384 */
#define INTEL_E1000_RCTL_SZ_8192 0x00020000 /* rx buffer size 8192 */
#define INTEL_E1000_RCTL_SZ_4096 0x00030000 /* rx buffer size 4096 */
#define INTEL_E1000_RCTL_VFE 0x00040000 /* vlan filter enable */
#define INTEL_E1000_RCTL_CFIEN 0x00080000 /* canonical form enable */
#define INTEL_E1000_RCTL_CFI 0x00100000 /* canonical form indicator */
#define INTEL_E1000_RCTL_DPF 0x00400000 /* Discard Pause Frames */
#define INTEL_E1000_RCTL_PMCF 0x00800000 /* pass MAC control frames */
#define INTEL_E1000_RCTL_BSEX 0x02000000 /* Buffer size extension */
#define INTEL_E1000_RCTL_SECRC 0x04000000 /* Strip Ethernet CRC */

#define INTEL_E1000_FCTTV 0x00170 /* Flow Control Transmit Timer Value - RW */
#define INTEL_E1000_TXCW 0x00178 /* TX Configuration Word - RW */
#define INTEL_E1000_RXCW 0x00180 /* RX Configuration Word - RW */
#define INTEL_E1000_PBA_ECC 0x01100 /* PBA ECC register */
#define INTEL_E1000_TCTL 0x00400 /* TX Control - RW */
/* Transmit Control */
#define INTEL_E1000_TCTL_EN 0x00000002 /* enable tx */
#define INTEL_E1000_TCTL_PSP 0x00000008 /* pad short packets */
#define INTEL_E1000_TCTL_CT 0x00000ff0 /* collision threshold */
#define INTEL_E1000_TCTL_COLD 0x003ff000 /* collision distance */
#define INTEL_E1000_TCTL_RTLC 0x01000000 /* Re-transmit on late collision */
#define INTEL_E1000_TCTL_MULR 0x10000000 /* Multiple request support */

#define INTEL_E1000_TCTL_EXT 0x00404 /* Extended TX Control - RW */
#define INTEL_E1000_TIPG 0x00410 /* TX Inter-packet gap -RW */
#define INTEL_E1000_AIT 0x00458 /* Adaptive Interframe Spacing Throttle - RW */
#define INTEL_E1000_LEDCTL 0x00E00 /* LED Control - RW */
#define INTEL_E1000_LEDMUX 0x08130 /* LED MUX Control */
#define INTEL_E1000_EXTCNF_CTRL 0x00f00 /* Extended Configuration Control */
#define INTEL_E1000_EXTCNF_CTRL_MDIO_SW_OWNERSHIP 0x20
#define INTEL_E1000_EXTCNF_CTRL_LCD_WRITE_ENABLE 0x1
#define INTEL_E1000_EXTCNF_CTRL_OEM_WRITE_ENABLE 0x8
#define INTEL_E1000_EXTCNF_CTRL_SWFLAG 0x20
#define INTEL_E1000_EXTCNF_CTRL_GATE_PHY_CFG 0x80

/* Low Power IDLE Control */
#define INTEL_E1000_LPIC_LPIET_SHIFT 24

#define INTEL_E1000_EXTCNF_SIZE 0x00f08
#define INTEL_E1000_EXTCNF_SIZE_EXT_PCIE_LENGTH_MASK 0x00ff0000
#define INTEL_E1000_EXTCNF_SIZE_EXT_PCIE_LENGTH_SHIFT 16
#define INTEL_E1000_EXTCNF_CTRL_EXT_CNF_POINTER_MASK 0x0FFF0000
#define INTEL_E1000_EXTCNF_CTRL_EXT_CNF_POINTER_SHIFT 16

#define INTEL_E1000_PHY_CTRL 0xf10
#define INTEL_E1000_PHY_CTRL_D0A_LPLU 0x2
#define INTEL_E1000_PHY_CTRL_NOND0A_LPLU 0x4
#define INTEL_E1000_PHY_CTRL_NOND0A_GBE_DISABLE 0x8
#define INTEL_E1000_PHY_CTRL_GBE_DISABLE 0x40

#define INTEL_E1000_POEMB E1000_PHY_CTRL /* Packet Buffer Allocation - RW */
#define INTEL_E1000_PBA 0x01000 /* Packet Buffer Allocation - RW */
#define INTEL_E1000_PBS 0x01008 /* Packet Buffer Size */
#define INTEL_E1000_PBECCSTS 0x0100C /* Packet Buffer ECC status - RW */
#define INTEL_E1000_IOSFPC 0x0f28 /* tx corrupted data */
#define INTEL_E1000_EEMNGCTL 0x01010 /* MNG EEprom Control */
#define INTEL_E1000_EEWR 0x0102C /* EEPROM Write Register - RW */
#define INTEL_E1000_FLOP 0x0103C /* Flash Opcode Register - RW */
#define INTEL_E1000_ERT 0x02008
#define INTEL_E1000_FCRTL 0x02160 /* Flow Control Receive Threshold Low - RW */
#define INTEL_E1000_FCRTH 0x02168 /* Flow Control Receive Threshold High - RW */
#define INTEL_E1000_PSRCTL 0x02170
#define INTEL_E1000_RDFH 0x02410
#define INTEL_E1000_RDFT 0x02418
#define INTEL_E1000_RDFHS 0x02420
#define INTEL_E1000_RDFTS 0x02428
#define INTEL_E1000_RDFPC 0x02430

#define INTEL_E1000_RDTR 0x02820
#define INTEL_E1000_RADV 0x0282C

#define INTEL_E1000_RXCSUM 0x05000 /* RX Checksum Control - RW */
#define INTEL_E1000_RFCTL 0x05008 /* Receive Filter Control*/

#define INTEL_E1000_KABGTXD 0x03004
#define INTEL_E1000_KABGTXD_BGSQLBIAS 0x050000

#define INTEL_E1000_H2ME 0x05b50
#define INTEL_E1000_H2ME_ULP 0x0800
#define INTEL_E1000_H2ME_ENFORCE_SETTINGS 0x01000

#define INTEL_E1000_SW_FW_SYNC 0x05B5C /* Software-Firmware Synchronization - RW */
#define INTEL_E1000_SWSM 0x05B50 /* SW Semaphore */
#define INTEL_E1000_FWSM 0x05B54 /* FW Semaphore */
#define INTEL_E1000_FWSM_ULP_CFG_DONE 0x0400 /* FW Semaphore */
#define INTEL_E1000_PCS_LCTL_FORCE_FCTRL 0x080
#define INTEL_E1000_PCS_LSTS_AN_COMPLETE 0x010000
#define INTEL_E1000_ICH8_LAN_INIT_TIMEOUT 1500

/* SW Semaphore Register */
#define INTEL_E1000_SWSM_SMBI 0x00000001 /* Driver Semaphore bit */
#define INTEL_E1000_SWSM_SWESMBI 0x00000002 /* FW Semaphore bit */

#define INTEL_E1000_EIAM 0x01530 /* Ext. Interrupt Ack Auto Clear Mask - RW */
#define INTEL_E1000_EIMC 0x01528 /* Ext. Interrupt Mask Clear - WO */
#define INTEL_E1000_EIAC 0x0152C /* Ext. Interrupt Auto Clear - RW */

#endif
