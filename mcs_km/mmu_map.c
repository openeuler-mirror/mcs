#include <linux/io.h>

#define PAGE_TABLE_OFFSET	0x1000
#define PAGE_TABLE_SIZE		0x6000

#define MEM_ATTR_UNCACHE_RWX	(_PAGE_PRESENT | _PAGE_RW | _PAGE_PCD | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_CACHE_RWX	(_PAGE_PRESENT | _PAGE_RW | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_CACHE_RX	(_PAGE_PRESENT | _PAGE_ACCESSED | _PAGE_DIRTY)
#define MEM_ATTR_WC_RWX		(_PAGE_PRESENT | _PAGE_RW | _PAGE_ACCESSED | _PAGE_DIRTY | _PAGE_PWT)

#define PAGE_SIZE_2M		0x200000
#define PAGE_SIZE_4K		0x1000

#define PPE_ADDR		0xa1003
#define PDE_ADDR		0xa2003
#define PTE_ADDR		0xa3003

#define PML_SHIFT		39
#define PPE_SHIFT		30
#define PDE_SHIFT		21
#define PTE_SHIFT		12

#define PML_OFFSET_GET(va)	(((va) >> PML_SHIFT << 3) & 0xFFF)
#define PPE_OFFSET_GET(va)	(((va) >> PPE_SHIFT << 3) & 0xFFF)
#define PDE_OFFSET_GET(va)	(((va) >> PDE_SHIFT << 3) & 0xFFF)
#define PTE_OFFSET_GET(va)	(((va) >> PTE_SHIFT << 3) & 0xFFF)

#define SYS_WRITE64(addr, val)	(*(unsigned long *)(addr) = (val))

typedef struct {
	unsigned long	va;
	unsigned long	pa;
	unsigned long	size;
	unsigned long	attr;
	unsigned long	page_size;
} mmu_map_info;

enum {
	BOOT_TABLE = 0,
	PAGE_TABLE,
	BAR_TABLE,
	DMA_TABLE,
	SHAREMEM_TABLE,
	LOG_TABLE,
	TEXT_TABLE,
	DATA_TABLE,
	TABLE_MAX
};

const char* mem_name[] = {
	"BOOT_TABLE",
	"PAGE_TABLE",
	"BAR_TABLE",
	"DMA_TABLE",
	"SHAREMEM_TABLE",
	"LOG_TABLE",
	"TEXT_TABLE",
	"DATA_TABLE",
};

static mmu_map_info clientos_map_info[TABLE_MAX] = {
	{
		// boottable
		.va = 0x0,
		.pa = 0x0,
		.size = 0x1000,
		.attr = MEM_ATTR_CACHE_RWX,
		.page_size = PAGE_SIZE_4K,
	}, {
		// pagetable
		.va = 0xa0000,
		.pa = 0xa0000,
		.size = 0x6000,
		.attr = MEM_ATTR_CACHE_RWX,
		.page_size = PAGE_SIZE_4K,
	}, {
		// bar
		.va = 0xf00008000,
		.pa = 0x0,
		.size = 0x100000,
		.attr = MEM_ATTR_UNCACHE_RWX,
		.page_size = PAGE_SIZE_4K,
	}, {
		// dma
		.va = 0xf00200000,
		.pa = 0x0,
		.size = 0x200000,
		.attr = MEM_ATTR_UNCACHE_RWX,
		.page_size = PAGE_SIZE_2M,
	}, {
		// sharemem
		.va = 0xf00400000,
		.pa = 0x0,
		.size = 0x2000000,
		.attr = MEM_ATTR_UNCACHE_RWX,
		.page_size = PAGE_SIZE_2M,
	}, {
		// log
		.va = 0xf02400000,
		.pa = 0x0,
		.size = 0x200000,
		.attr = MEM_ATTR_UNCACHE_RWX,
		.page_size = PAGE_SIZE_2M,
	}, {
		// text
		.va = 0xf02600000,
		.pa = 0x0,
		.size = 0x400000,
		.attr = MEM_ATTR_CACHE_RX,
		.page_size = PAGE_SIZE_2M,
	}, {
		// data
		.va = 0xf02a00000,
		.pa = 0x0,
		.size = 0x1000000,
		.attr = MEM_ATTR_CACHE_RWX,
		.page_size = PAGE_SIZE_2M,
	}
};

void set_bar_addr(unsigned long phy_addr)
{
	clientos_map_info[BAR_TABLE].pa = phy_addr;
}
EXPORT_SYMBOL_GPL(set_bar_addr);

static void mem_mmu_page_table(mmu_map_info *map_info)
{
	unsigned long mapped, val;
	unsigned long table_addr, pml, ppe, pde, pte;
	unsigned long pml_off, ppe_off, pde_off, pte_off;
	int large_page = (map_info->page_size == PAGE_SIZE_2M) ? 1 : 0;

	table_addr = clientos_map_info[PAGE_TABLE].pa;
	pml = (unsigned long)memremap(table_addr, PAGE_TABLE_SIZE, MEMREMAP_WB);
	if (!pml) {
		pr_err("memremap failed\n");
		return;
	}

	ppe = pml + PAGE_TABLE_OFFSET;
	pde = ppe + PAGE_TABLE_OFFSET;
	pte = pde + PAGE_TABLE_OFFSET;

	for (mapped = 0; mapped < map_info->size; mapped += map_info->page_size) {
		pml_off = PML_OFFSET_GET(map_info->va + mapped);
		SYS_WRITE64(pml + pml_off, PPE_ADDR);

		ppe_off = PPE_OFFSET_GET(map_info->va + mapped);
		SYS_WRITE64(ppe + ppe_off, PDE_ADDR);

		pde_off = PDE_OFFSET_GET(map_info->va + mapped);
		if (large_page) {
			val = map_info->attr | _PAGE_PAT | ((map_info->pa + mapped) >> PDE_SHIFT << PDE_SHIFT);
			SYS_WRITE64(pde + pde_off, val);
			continue;
		} else {
			SYS_WRITE64(pde + pde_off, PTE_ADDR);
		}

		pte_off = PTE_OFFSET_GET(map_info->va + mapped);
		val = map_info->attr | ((map_info->pa + mapped) >> PTE_SHIFT << PTE_SHIFT);
		SYS_WRITE64(pte + pte_off, val);
	}
	memunmap((void *)pml);
}

void mem_map_info_set(unsigned long loadaddr)
{
	void *table_base;
	unsigned long *log_table_base;
	int i;
	// todo: ALIGN_UP_2M
	// unsigned long phy_addr = ALIGN_UP_2M(loadaddr);
	unsigned long phy_addr = loadaddr;

	for(i = TEXT_TABLE - 1; i >= DMA_TABLE; i--) {
		phy_addr -= (clientos_map_info[i].size >= PAGE_SIZE_2M) ? clientos_map_info[i].size : PAGE_SIZE_2M;
		clientos_map_info[i].pa = phy_addr;
	}

	phy_addr = loadaddr;
	for(i = TEXT_TABLE; i < TABLE_MAX; i++) {
		clientos_map_info[i].pa = phy_addr;
		phy_addr += (clientos_map_info[i].size >= PAGE_SIZE_2M) ? clientos_map_info[i].size : PAGE_SIZE_2M;
	}

	table_base = memremap(clientos_map_info[PAGE_TABLE].pa, PAGE_TABLE_SIZE, MEMREMAP_WB);
	if (table_base == NULL) {
		pr_err("mem_map_info_set failed\n");
		return;
	}

	memset(table_base, 0, PAGE_TABLE_SIZE);
	memunmap(table_base);

	for (i = 0; i < TABLE_MAX; i++) {
		/* bar 分区的物理地址需要外部接口配置，如果没有配置（pa == 0），则不进行页表映射 */
		if ((i == BAR_TABLE) && (clientos_map_info[i].pa == 0)) {
			continue;
		}
		pr_info("map %s: pa 0x%lx, va 0x%lx, size 0x%lx, pagesize 0x%lx\n",
			mem_name[i], clientos_map_info[i].pa, clientos_map_info[i].va,
			clientos_map_info[i].size, clientos_map_info[i].page_size);
		mem_mmu_page_table(&clientos_map_info[i]);
	}

	/* In order to init the shm device, uniproton needs to know the actual physical
	 address of the SHAREMEM_TABLE, so we will write it in the log table. */
	log_table_base = memremap(clientos_map_info[LOG_TABLE].pa, 0x200000, MEMREMAP_WT);
	if (log_table_base == NULL) {
		pr_err("mem_map_info_set memremap failed\n");
		return;
	}
	log_table_base[0] = clientos_map_info[SHAREMEM_TABLE].pa;
	memunmap(log_table_base);
}
