/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef SIMPLUS_SIMAKA_CARD_H_
#define SIMPLUS_SIMAKA_CARD_H_

#include <simaka_card.h>

#include "simplus_simaka_agent.h"

typedef struct simplus_simaka_card_t simplus_simaka_card_t;

struct simplus_simaka_card_t {
	simaka_card_t card;
	void (*destroy)(simplus_simaka_card_t *this);
};

simplus_simaka_card_t *simplus_simaka_card_create(
	const simplus_simaka_target_t *target,
	const char *expected_identity);

#endif /* SIMPLUS_SIMAKA_CARD_H_ */
