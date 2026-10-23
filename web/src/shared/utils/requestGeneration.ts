export interface RequestGeneration {
  current(): number;
  bump(): void;
  isCurrent(_token: number): boolean;
}

export function createRequestGeneration(): RequestGeneration {
  let value = 0;
  return {
    current: () => value,
    bump: () => {
      value += 1;
    },
    isCurrent: (token: number) => token === value,
  };
}
