import{f as g}from"./format-length-DEgreG5f.js";import{J as v,a0 as l,I as m,d as b,K as h,bj as y,L as d,M as C,O as _,a2 as z,y as a,bz as $}from"./index-5UA4KEVr.js";var x=v("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[m("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),m("svg",{height:"1em",width:"1em"})]);const R={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},P=b({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:R,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:n}=_(e),t=h("Icon","-icon",x,$,e,o),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=t.value;if(r!==void 0){const{color:f,[`opacity${r}Depth`]:u}=p;return{"--n-bezier":c,"--n-color":f,"--n-opacity":u}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?z("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:o,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:g(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:o,mergedClsPrefix:n,component:t,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&y("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),d("i",C(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:o,[`${n}-icon--color-transition`]:o!==void 0}],style:[this.cssVars,this.mergedStyle]}),t?d(t):this.$slots.default?.())}});export{P as N};
