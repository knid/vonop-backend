import { create } from 'zustand';

interface AppState {
  // Onboarding state example
  isOnboarded: boolean;
  setIsOnboarded: (value: boolean) => void;

  // App-ready state example
  isAppReady: boolean;
  setIsAppReady: (value: boolean) => void;
}

export const useAppStore = create<AppState>((set) => ({
  isOnboarded: false,
  setIsOnboarded: (value) => set({ isOnboarded: value }),

  isAppReady: false,
  setIsAppReady: (value) => set({ isAppReady: value }),
}));

// ------------------------------------------------------------------
// Persistence (optional)
// ------------------------------------------------------------------
// To persist slices of this store across app restarts, wrap the store
// with `zustand/middleware`'s `persist` middleware and provide a
// storage adapter.
//
// Example with expo-secure-store:
//
//   import { create } from 'zustand';
//   import { persist, createJSONStorage } from 'zustand/middleware';
//   import * as SecureStore from 'expo-secure-store';
//
//   const secureStorage = {
//     getItem: async (name: string) => {
//       const value = await SecureStore.getItemAsync(name);
//       return value ? JSON.parse(value) : null;
//     },
//     setItem: async (name: string, value: unknown) => {
//       await SecureStore.setItemAsync(name, JSON.stringify(value));
//     },
//     removeItem: async (name: string) => {
//       await SecureStore.deleteItemAsync(name);
//     },
//   };
//
//   export const useAppStore = create<AppState>()(
//     persist(
//       (set) => ({
//         isOnboarded: false,
//         setIsOnboarded: (value) => set({ isOnboarded: value }),
//         isAppReady: false,
//         setIsAppReady: (value) => set({ isAppReady: value }),
//       }),
//       {
//         name: 'app-store',
//         storage: createJSONStorage(() => secureStorage),
//         partialize: (state) => ({ isOnboarded: state.isOnboarded }),
//       }
//     )
//   );
