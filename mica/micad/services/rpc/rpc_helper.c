/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdlib.h>
#include <string.h>
#include <netdb.h>
#include <sys/poll.h>
#include <sys/select.h>
#include <sys/un.h>
#include <stdint.h>

#include "rpc_internal_model.h"
#include "rpc_server_internal.h"
#include "rpc_err.h"

static char *__strdup(const char *s)
{
	size_t l = strlen(s);
	char *d = malloc(l+1);

	if (!d)
		return NULL;

	return memcpy(d, s, l+1);
}

void freeaddrlist(struct addrinfo *ai)
{
	struct addrinfo *p;

	while (ai != NULL) {
		p = ai;
		ai = ai->ai_next;
		free(p->ai_canonname);
		free(p);
	}
}

int encode_addrlist(const struct addrinfo *ai, char *buf, int *buflen)
{
	int len = 0, bi = 0, cnt = 0, aclen = 0;
	const struct addrinfo *p = ai;
	int hlen = sizeof(iaddrinfo_t) - sizeof(int);

	if (ai == NULL || buf == NULL || buflen == NULL) {
		return -RPC_EINVAL;
	}
	while (p != NULL) {
		len += hlen + p->ai_addrlen + sizeof(int);
		if (p->ai_canonname != NULL) {
			len += strlen(p->ai_canonname) + 1;
		}
		p = p->ai_next;
	}
	if (len > *buflen) {
		return -RPC_EOVERLONG;
	}
	*buflen = len;
	p = ai;
	while (p != NULL) {
		memcpy(&buf[bi], p, hlen);
		bi += hlen;
		aclen = 0;
		if (p->ai_canonname != NULL) {
			aclen = strlen(p->ai_canonname) + 1;
		}
		memcpy(&buf[bi], &aclen, sizeof(int));
		bi += sizeof(int);
		if (p->ai_addr != NULL && p->ai_addrlen > 0) {
			memcpy(&buf[bi], p->ai_addr, p->ai_addrlen);
			bi += p->ai_addrlen;
		}
		if (aclen > 0) {
			memcpy(&buf[bi], p->ai_canonname, aclen);
			bi += aclen;
		}
		p = p->ai_next;
		cnt++;
	}

	return cnt;
}

int decode_addrlist(const char *buf, int cnt, int buflen, struct addrinfo **out)
{
	int bi = 0, aclen = 0, ret = 0;
	struct addrinfo *p = NULL, **pp = out;
	int hlen = sizeof(iaddrinfo_t) - sizeof(int);

	*out = p;
	for (int i = 0; i < cnt; i++) {
		struct addrinfo addr;

		memcpy(&addr, &buf[bi], hlen);
		if (addr.ai_addrlen < 0) {
			ret = -RPC_ECORRUPTED;
			goto clean;
		}
		*pp = p = (struct addrinfo *)malloc(sizeof(struct addrinfo) + addr.ai_addrlen);
		if (p == NULL) {
			ret = -RPC_ENOMEM;
			goto clean;
		}
		if (bi + hlen >= buflen) {
			ret = -RPC_EOVERLONG;
			goto clean;
		}
		memcpy(p, &buf[bi], hlen);
		bi += hlen;
		if (bi + sizeof(int) >= buflen) {
			ret = -RPC_EOVERLONG;
			goto clean;
		}
		memcpy(&aclen, &buf[bi], sizeof(int));
		bi += sizeof(int);
		p->ai_addr = (void *)&p[1];
		if (addr.ai_addrlen > 0) {
			if (bi + addr.ai_addrlen >= buflen) {
				ret = -RPC_EOVERLONG;
				goto clean;
			}
			memcpy(p->ai_addr, &buf[bi], addr.ai_addrlen);
			bi += addr.ai_addrlen;
		}
		p->ai_canonname = NULL;
		if (aclen > 0) {
			if (&buf[bi] == NULL) {
				ret = -RPC_ECORRUPTED;
				goto clean;
			}
			p->ai_canonname = __strdup(&buf[bi]);
			bi += aclen;
		}

		p->ai_next = NULL;
		pp = &(p->ai_next);
	}
	return 0;
clean:
	freeaddrlist(*out);
	return ret;
}

int decode_hostent(struct hostent **ppht, char *src_buf, int buflen)
{
	ihostent_t ih;
	int tlen, dst_idx, src_idx, i, slen;
	struct hostent *pht;
	char *dst_buf, **aliases, **addr;

	if (ppht == NULL || src_buf == NULL || buflen < sizeof(ihostent_t)) {
		return -RPC_EINVAL;
	}
	memcpy(&ih, src_buf, sizeof(ih));
	if (ih.h_name_idx > ih.h_aliases_idx ||
		ih.h_aliases_idx > ih.h_addr_list_idx) {
		return -RPC_ECORRUPTED;
	}
	tlen = buflen + sizeof(char *) * (ih.aliaslen + ih.addrlen + 2) -
	sizeof(ihostent_t) + sizeof(struct hostent);
	dst_idx = sizeof(char *) * (ih.aliaslen + ih.addrlen + 2);
	src_idx = sizeof(ihostent_t);
	pht = (struct hostent *)malloc(tlen);
	if (pht == NULL) {
		return -RPC_ENOMEM;
	}
	dst_buf = (char *)(pht + 1);
	memcpy(&dst_buf[dst_idx], &src_buf[src_idx], buflen - sizeof(ihostent_t));

	pht->h_length = ih.h_length;
	pht->h_addrtype = ih.h_addrtype;
	if (ih.h_name_idx == ih.h_aliases_idx) {
		pht->h_name = NULL;
	} else {
		pht->h_name = &dst_buf[dst_idx];
		dst_idx += ih.h_aliases_idx - ih.h_name_idx;
	}
	aliases = pht->h_aliases = (char **)dst_buf;
	for (i = 0; i < ih.aliaslen; i++) {
		aliases[i] = &dst_buf[dst_idx];
		if (aliases[i] == NULL) {
			free(pht);
			return -RPC_ECORRUPTED;
		}
		slen = strlen(aliases[i]) + 1;
		dst_idx += slen;
	}
	aliases[i] = NULL;
	addr = pht->h_addr_list = (char **)&dst_buf[sizeof(char *) * (ih.aliaslen + 1)];
	slen = pht->h_length;
	for (i = 0 ; i < ih.addrlen; i++) {
		addr[i] = &dst_buf[dst_idx];
		dst_idx += slen;
	}
	addr[i] = NULL;
	*ppht = pht;
	return 0;
}

int encode_hostent(struct hostent *ht, char *buf, int buflen)
{
	int tlen = sizeof(ihostent_t), len = 0;
	ihostent_t ih;
	char **p;

	if (ht == NULL || buf == NULL) {
		return -RPC_EINVAL;
	}
	ih.aliaslen = 0;
	ih.addrlen = 0;
	ih.h_name_idx = tlen;
	ih.h_addrtype = ht->h_addrtype;
	ih.h_length = ht->h_length;
	if (ht->h_name != NULL) {
		len = strlen(ht->h_name) + 1;
		if (tlen + len >= buflen) {
			return -RPC_EOVERLONG;
		}
		memcpy(&buf[tlen], ht->h_name, len);
		tlen += len;
	}
	ih.h_aliases_idx = tlen;
	if (ht->h_aliases != NULL) {
		p = ht->h_aliases;
		for (int i = 0; p[i] != NULL; i++) {
			len = strlen(p[i]) + 1;
			if (tlen + len >= buflen) {
				return -RPC_EOVERLONG;
			}
			memcpy(&buf[tlen], p[i], len);
			ih.aliaslen++;
			tlen += len;
		}
	}
	ih.h_addr_list_idx = tlen;
	if (ht->h_addr_list != NULL) {
		len = ht->h_length;
		p = ht->h_addr_list;
		for (int i = 0; p[i] != NULL; i++) {
			if (tlen + len >= buflen) {
				return -RPC_EOVERLONG;
			}
			memcpy(&buf[tlen], p[i], len);
			ih.addrlen++;
			tlen += len;
		}
	}
	memcpy(buf, &ih, sizeof(ih));
	return tlen;
}
