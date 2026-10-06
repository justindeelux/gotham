import{f as g}from"./format-length-sm17q4Ll.js";import{_ as v,ar as l,Z as d,d as y,$ as h,cV as C,a0 as m,a1 as _,a3 as b,at as $,z as a,dc as z}from"./index-BoRrRIw2.js";var x=v("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[d("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),d("svg",{height:"1em",width:"1em"})]);const R={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},w=y({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:R,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:n}=b(e),o=h("Icon","-icon",x,z,e,t),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=o.value;if(r!==void 0){const{color:f,[`opacity${r}Depth`]:u}=p;return{"--n-bezier":c,"--n-color":f,"--n-opacity":u}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?$("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:t,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:g(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:t,mergedClsPrefix:n,component:o,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&C("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),m("i",_(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:t,[`${n}-icon--color-transition`]:t!==void 0}],style:[this.cssVars,this.mergedStyle]}),o?m(o):this.$slots.default?.())}});export{w as N};
