/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef SIMPLUS_SIMAKA_APN_H_
#define SIMPLUS_SIMAKA_APN_H_

#include <bus/listeners/listener.h>

typedef struct simplus_simaka_apn_t simplus_simaka_apn_t;

struct simplus_simaka_apn_t {
	listener_t listener;
	void (*destroy)(simplus_simaka_apn_t *this);
};

simplus_simaka_apn_t *simplus_simaka_apn_create(void);

#endif /* SIMPLUS_SIMAKA_APN_H_ */
