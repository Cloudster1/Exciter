import { useCallback, useRef } from "react";
import { useMutation as useQueryMutation, type DefaultError, type MutateOptions, type UseMutationOptions, type UseMutationResult } from "@tanstack/react-query";

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: {
      /** The caller tells the reader about a failure itself, so the shell's toast stays quiet. */
      handlesError?: boolean;
      /** The caller answers a 401 itself, such as signing in or out. */
      handlesUnauthorized?: boolean;
    };
  }
}

/**
 * TanStack's useMutation, which also tells the shell whether its caller shows a failure.
 * Reading error, isError or mutateAsync, or passing mutate an onError, counts as showing it.
 */
export function useMutation<TData = unknown, TError = DefaultError, TVariables = void, TOnMutateResult = unknown>(
  options: UseMutationOptions<TData, TError, TVariables, TOnMutateResult>,
): UseMutationResult<TData, TError, TVariables, TOnMutateResult> {
  const shown = useRef(false);
  const declared = options.meta?.handlesError === true;
  // A getter, because the caller reads the error after this hook has handed over its options.
  const meta = {
    ...options.meta,
    get handlesError() {
      return declared || shown.current;
    },
  };
  const result = useQueryMutation({ ...options, meta });
  const show = () => {
    shown.current = true;
  };
  const { mutate: run } = result;
  const mutate = useCallback(
    (variables: TVariables, mutateOptions?: MutateOptions<TData, TError, TVariables, TOnMutateResult>) => {
      if (mutateOptions?.onError) shown.current = true;
      run(variables, mutateOptions);
    },
    [run],
  );
  return {
    ...result,
    mutate,
    get error() {
      show();
      return result.error;
    },
    get isError() {
      show();
      return result.isError;
    },
    get failureReason() {
      show();
      return result.failureReason;
    },
    get mutateAsync() {
      show();
      return result.mutateAsync;
    },
  } as UseMutationResult<TData, TError, TVariables, TOnMutateResult>;
}
