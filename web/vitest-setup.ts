import { cleanup } from '@testing-library/svelte';
import { afterEach } from 'vitest';

// Testing Library only registers its own cleanup when Vitest globals are enabled, and they are not
// here: each test file imports what it uses.
afterEach(cleanup);
