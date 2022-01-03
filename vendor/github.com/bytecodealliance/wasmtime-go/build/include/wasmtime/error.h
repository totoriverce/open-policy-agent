/**
 * \file wasmtime/error.h
 *
 * \brief Definition and accessors of #wasmtime_error_t
 */

#ifndef WASMTIME_ERROR_H
#define WASMTIME_ERROR_H

#include <wasm.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * \typedef wasmtime_error_t
 * \brief Convenience alias for #wasmtime_error
 *
 * \struct wasmtime_error
 * \brief Errors generated by Wasmtime.
 * \headerfile wasmtime/error.h
 *
 * This opaque type represents an error that happened as part of one of the
 * functions below. Errors primarily have an error message associated with them
 * at this time, which you can acquire by calling #wasmtime_error_message.
 *
 * Errors are safe to share across threads and must be deleted with
 * #wasmtime_error_delete.
 */
typedef struct wasmtime_error wasmtime_error_t;

/**
 * \brief Deletes an error.
 */
WASM_API_EXTERN void wasmtime_error_delete(wasmtime_error_t *error);

/**
 * \brief Returns the string description of this error.
 *
 * This will "render" the error to a string and then return the string
 * representation of the error to the caller. The `message` argument should be
 * uninitialized before this function is called and the caller is responsible
 * for deallocating it with #wasm_byte_vec_delete afterwards.
 */
WASM_API_EXTERN void wasmtime_error_message(
    const wasmtime_error_t *error,
    wasm_name_t *message
);

#ifdef __cplusplus
}  // extern "C"
#endif

#endif // WASMTIME_ERROR_H
